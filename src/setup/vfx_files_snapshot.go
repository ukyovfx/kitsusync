package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"app/src/api/kitsu"
	"app/src/api/vfxfiles"
)

const (
	vfxSnapshotPageSize = 500
	vfxSnapshotMaxPages = 1000
	vfxSnapshotMaxItems = 500000
)

type vfxFilesSnapshotObject struct {
	ObjectType     string  `json:"object_type"`
	ID             string  `json:"id"`
	ParentID       *string `json:"parent_id"`
	Name           string  `json:"name"`
	Code           *string `json:"code,omitempty"`
	TaskTypeID     *string `json:"task_type_id,omitempty"`
	UpdatedAt      *string `json:"updated_at,omitempty"`
	IncludedInSync bool    `json:"included_in_sync"`
}

type vfxFilesSnapshotScope struct {
	Mode       string
	Episodes   bool
	Sequences  bool
	Shots      bool
	Assets     bool
	Tasks      bool
	TaskTypes  []string
	IncludeIDs []string
	ExcludeIDs []string
}

type vfxFilesSnapshotSourceData struct {
	Production vfxFilesSnapshotRawObject
	Episodes   []vfxFilesSnapshotRawObject
	Sequences  []vfxFilesSnapshotRawObject
	Shots      []vfxFilesSnapshotRawObject
	Assets     []vfxFilesSnapshotRawObject
	Tasks      []vfxFilesSnapshotRawObject
	TaskTypes  []kitsu.TaskType
}

type vfxFilesSnapshotRawObject struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	ProjectID  string          `json:"project_id"`
	ParentID   string          `json:"parent_id"`
	EpisodeID  string          `json:"episode_id"`
	SequenceID string          `json:"sequence_id"`
	EntityID   string          `json:"entity_id"`
	TaskTypeID string          `json:"task_type_id"`
	UpdatedAt  string          `json:"updated_at"`
	Code       json.RawMessage `json:"code"`
}

type vfxFilesKitsuSnapshotReader interface {
	BuildVFXFilesSnapshot(context.Context, string, vfxFilesSnapshotScope) ([]vfxFilesSnapshotObject, string, error)
}

// buildVFXFilesSnapshot is deterministic and side-effect free. The source
// fingerprint is content-derived because Kitsu list endpoints do not promise
// a collection ETag; it is not represented as an upstream HTTP validator.
func buildVFXFilesSnapshot(productionID string, scope vfxFilesSnapshotScope, source vfxFilesSnapshotSourceData) ([]vfxFilesSnapshotObject, string, error) {
	productionID = strings.TrimSpace(productionID)
	if !vfxfiles.ValidID(productionID) || scope.Mode != "new_production" && scope.Mode != "adopt_existing" && scope.Mode != "future_only" {
		return nil, "", errors.New("snapshot scope is invalid")
	}
	objects := make(map[string]vfxFilesSnapshotObject)
	add := func(kind, id, parent, name string, code json.RawMessage, taskType, updated string, selected bool) error {
		id = strings.ToLower(strings.TrimSpace(id))
		if !vfxfiles.ValidID(id) || len([]rune(strings.TrimSpace(name))) == 0 || len([]rune(name)) > 240 || strings.ContainsAny(name, "\x00\r\n") {
			return errors.New("Kitsu snapshot contains an invalid object")
		}
		if len(objects) >= vfxSnapshotMaxItems || objects[id].ID != "" {
			return errors.New("Kitsu snapshot object limit or identity uniqueness was violated")
		}
		var parentID *string
		if strings.TrimSpace(parent) != "" {
			value := strings.ToLower(strings.TrimSpace(parent))
			if !vfxfiles.ValidID(value) {
				return errors.New("Kitsu snapshot parent identity is invalid")
			}
			parentID = &value
		}
		var codeValue *string
		if len(code) > 0 && string(code) != "null" {
			var value string
			if err := json.Unmarshal(code, &value); err != nil || len([]rune(value)) > 120 || strings.ContainsAny(value, "\x00\r\n") {
				return errors.New("Kitsu snapshot code is invalid")
			}
			codeValue = &value
		}
		var taskTypeValue *string
		if taskType != "" {
			value := strings.ToLower(strings.TrimSpace(taskType))
			if !vfxfiles.ValidID(value) {
				return errors.New("Kitsu snapshot Task Type identity is invalid")
			}
			taskTypeValue = &value
		}
		var updatedValue *string
		if updated != "" {
			parsed, err := parseKitsuSnapshotTimestamp(updated)
			if err != nil {
				return errors.New("Kitsu snapshot timestamp is invalid")
			}
			value := parsed.Format(time.RFC3339Nano)
			updatedValue = &value
		}
		objects[id] = vfxFilesSnapshotObject{ObjectType: kind, ID: id, ParentID: parentID, Name: strings.TrimSpace(name), Code: codeValue, TaskTypeID: taskTypeValue, UpdatedAt: updatedValue, IncludedInSync: selected}
		return nil
	}
	if source.Production.ID != productionID || strings.TrimSpace(source.Production.ProjectID) != "" && !strings.EqualFold(source.Production.ProjectID, productionID) {
		return nil, "", errors.New("Kitsu Production response does not match the requested Production")
	}
	if err := add("production", source.Production.ID, "", source.Production.Name, source.Production.Code, "", source.Production.UpdatedAt, true); err != nil {
		return nil, "", err
	}
	addRows := func(kind string, rows []vfxFilesSnapshotRawObject, chosen bool, parent func(vfxFilesSnapshotRawObject) string, taskType string) error {
		for _, row := range rows {
			if row.ProjectID != "" && !strings.EqualFold(row.ProjectID, productionID) {
				return errors.New("Kitsu returned an object from another Production")
			}
			selected := chosen && scope.Mode != "future_only" && selectedByScope(row.ID, scope)
			if kind == "task" {
				if !scope.Tasks || scope.Mode == "future_only" || len(scope.TaskTypes) > 0 && !containsID(scope.TaskTypes, taskType) {
					selected = false
				}
			}
			if err := add(kind, row.ID, parent(row), row.Name, row.Code, taskType, row.UpdatedAt, selected); err != nil {
				return err
			}
		}
		return nil
	}
	if err := addRows("episode", source.Episodes, scope.Episodes, func(row vfxFilesSnapshotRawObject) string { return productionID }, ""); err != nil {
		return nil, "", err
	}
	if err := addRows("sequence", source.Sequences, scope.Sequences, func(row vfxFilesSnapshotRawObject) string {
		if row.EpisodeID != "" {
			return row.EpisodeID
		}
		return productionID
	}, ""); err != nil {
		return nil, "", err
	}
	if err := addRows("shot", source.Shots, scope.Shots, func(row vfxFilesSnapshotRawObject) string {
		if row.ParentID != "" {
			return row.ParentID
		}
		return row.SequenceID
	}, ""); err != nil {
		return nil, "", err
	}
	if err := addRows("asset", source.Assets, scope.Assets, func(row vfxFilesSnapshotRawObject) string {
		if row.ParentID != "" {
			return row.ParentID
		}
		return productionID
	}, ""); err != nil {
		return nil, "", err
	}
	for _, task := range source.Tasks {
		parent := task.EntityID
		if parent == "" {
			parent = task.ParentID
		}
		if parent == "" {
			return nil, "", errors.New("Kitsu Task has no entity identity")
		}
		if err := addRows("task", []vfxFilesSnapshotRawObject{task}, scope.Tasks, func(vfxFilesSnapshotRawObject) string { return parent }, task.TaskTypeID); err != nil {
			return nil, "", err
		}
	}
	for _, taskType := range source.TaskTypes {
		if err := add("task_type", taskType.ID, "", taskType.Name, nil, "", "", false); err != nil {
			return nil, "", err
		}
	}
	if err := markRequiredAncestors(objects); err != nil {
		return nil, "", err
	}
	result := make([]vfxFilesSnapshotObject, 0, len(objects))
	for _, object := range objects {
		result = append(result, object)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ObjectType == result[j].ObjectType {
			return result[i].ID < result[j].ID
		}
		return result[i].ObjectType < result[j].ObjectType
	})
	if len(result) == 0 || len(result) > vfxSnapshotMaxItems {
		return nil, "", errors.New("Kitsu snapshot is empty or exceeds the contract limit")
	}
	canonical, err := canonicalVFXSnapshotJSON(result)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(canonical)
	return result, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func parseKitsuSnapshotTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	// Zou stores UTC timestamps as naive DateTime values and serializes them
	// without an offset. Its get_utc_now_datetime() explicitly creates UTC
	// before removing tzinfo; normalize only that established wire form.
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("Kitsu timestamp is not RFC3339 or Zou UTC format")
}

func markRequiredAncestors(objects map[string]vfxFilesSnapshotObject) error {
	for _, object := range objects {
		if !object.IncludedInSync {
			continue
		}
		parent := object.ParentID
		seen := map[string]bool{object.ID: true}
		for parent != nil {
			if seen[*parent] {
				return errors.New("Kitsu snapshot hierarchy contains a cycle")
			}
			seen[*parent] = true
			ancestor, exists := objects[*parent]
			if !exists {
				return errors.New("Kitsu snapshot parent is missing")
			}
			if !ancestor.IncludedInSync {
				ancestor.IncludedInSync = false
				objects[*parent] = ancestor
			}
			parent = ancestor.ParentID
		}
		if object.ObjectType == "task" {
			if object.TaskTypeID == nil {
				return errors.New("Kitsu Task has no Task Type identity")
			}
			if _, ok := objects[*object.TaskTypeID]; !ok {
				return errors.New("Kitsu Task Type metadata is missing")
			}
		}
	}
	return nil
}

func selectedByScope(id string, scope vfxFilesSnapshotScope) bool {
	if containsID(scope.ExcludeIDs, id) {
		return false
	}
	return len(scope.IncludeIDs) == 0 || containsID(scope.IncludeIDs, id)
}

func containsID(values []string, id string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(id)) {
			return true
		}
	}
	return false
}

func canonicalVFXSnapshotJSON(value any) ([]byte, error) {
	if objects, ok := value.([]vfxFilesSnapshotObject); ok {
		canonicalObjects := make([]map[string]any, 0, len(objects))
		for _, object := range objects {
			item := map[string]any{
				"object_type":      object.ObjectType,
				"id":               object.ID,
				"parent_id":        object.ParentID,
				"name":             object.Name,
				"included_in_sync": object.IncludedInSync,
			}
			if object.Code != nil {
				item["code"] = *object.Code
			}
			if object.TaskTypeID != nil {
				item["task_type_id"] = *object.TaskTypeID
			}
			if object.UpdatedAt != nil {
				item["updated_at"] = *object.UpdatedAt
			}
			canonicalObjects = append(canonicalObjects, item)
		}
		value = canonicalObjects
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	data := bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
	data = bytes.ReplaceAll(data, []byte(`\u2028`), []byte("\u2028"))
	data = bytes.ReplaceAll(data, []byte(`\u2029`), []byte("\u2029"))
	return append([]byte(nil), data...), nil
}

func pageVFXFilesSnapshot(objects []vfxFilesSnapshotObject) ([][]vfxFilesSnapshotObject, []string, string, error) {
	if len(objects) == 0 || len(objects) > vfxSnapshotMaxItems {
		return nil, nil, "", errors.New("snapshot object count is outside contract bounds")
	}
	fullBytes, err := canonicalVFXSnapshotJSON(objects)
	if err != nil {
		return nil, nil, "", err
	}
	fullHash := sha256.Sum256(fullBytes)
	pageCount := (len(objects) + vfxSnapshotPageSize - 1) / vfxSnapshotPageSize
	if pageCount > vfxSnapshotMaxPages {
		return nil, nil, "", errors.New("snapshot page count exceeds contract bounds")
	}
	pages := make([][]vfxFilesSnapshotObject, 0, pageCount)
	hashes := make([]string, 0, pageCount)
	for start := 0; start < len(objects); start += vfxSnapshotPageSize {
		end := start + vfxSnapshotPageSize
		if end > len(objects) {
			end = len(objects)
		}
		page := objects[start:end]
		data, err := canonicalVFXSnapshotJSON(page)
		if err != nil {
			return nil, nil, "", err
		}
		digest := sha256.Sum256(data)
		pages = append(pages, page)
		hashes = append(hashes, hex.EncodeToString(digest[:]))
	}
	return pages, hashes, hex.EncodeToString(fullHash[:]), nil
}

func (a vfxFilesKitsuSessionAuthorizer) BuildVFXFilesSnapshot(ctx context.Context, productionID string, scope vfxFilesSnapshotScope) ([]vfxFilesSnapshotObject, string, error) {
	if _, allowed, err := a.AuthorizeProductionAdmin(ctx, productionID); err != nil || !allowed {
		return nil, "", errors.New("Kitsu Production authorization is unavailable")
	}
	var source vfxFilesSnapshotSourceData
	base := "/data/projects/" + url.PathEscape(productionID)
	if err := fetchVFXFilesKitsuJSON(ctx, a.connection, a.token, base, &source.Production); err != nil {
		return nil, "", err
	}
	for _, endpoint := range []struct {
		name   string
		target *[]vfxFilesSnapshotRawObject
	}{
		{"episodes", &source.Episodes}, {"sequences", &source.Sequences}, {"shots", &source.Shots},
	} {
		if err := fetchVFXFilesKitsuJSON(ctx, a.connection, a.token, base+"/"+endpoint.name, endpoint.target); err != nil {
			return nil, "", err
		}
	}
	if err := fetchVFXFilesKitsuArray(ctx, a.connection, a.token, base+"/assets", &source.Assets); err != nil {
		return nil, "", err
	}
	if err := fetchVFXFilesKitsuJSON(ctx, a.connection, a.token, base+"/task-types", &source.TaskTypes); err != nil {
		return nil, "", err
	}
	if err := fetchVFXFilesKitsuTaskPages(ctx, a.connection, a.token, base+"/tasks", &source.Tasks); err != nil {
		return nil, "", err
	}
	if err := fillVFXFilesTaskTypeMetadata(ctx, a.connection, a.token, source.TaskTypes, source.Tasks, &source.TaskTypes); err != nil {
		return nil, "", err
	}
	return buildVFXFilesSnapshot(productionID, scope, source)
}

// Project Task Type settings may omit legacy or otherwise existing Tasks.
// Resolve only referenced missing IDs from Kitsu's canonical Task Type
// catalog; unrelated studio Task Types are not copied into the snapshot.
func fillVFXFilesTaskTypeMetadata(ctx context.Context, connection KitsuURLModel, token string, configured []kitsu.TaskType, tasks []vfxFilesSnapshotRawObject, out *[]kitsu.TaskType) error {
	present := make(map[string]struct{}, len(configured))
	needed := make(map[string]struct{})
	for _, taskType := range configured {
		id := strings.ToLower(strings.TrimSpace(taskType.ID))
		if !vfxfiles.ValidID(id) {
			return errors.New("Kitsu Project Task Type metadata contains an invalid identity")
		}
		if _, duplicate := present[id]; duplicate {
			return errors.New("Kitsu Project Task Type metadata contains duplicate identities")
		}
		present[id] = struct{}{}
	}
	for _, task := range tasks {
		id := strings.ToLower(strings.TrimSpace(task.TaskTypeID))
		if id == "" {
			continue
		}
		if !vfxfiles.ValidID(id) {
			return errors.New("Kitsu Task contains an invalid Task Type identity")
		}
		if _, ok := present[id]; !ok {
			needed[id] = struct{}{}
		}
	}
	if len(needed) == 0 {
		return nil
	}
	var catalog []kitsu.TaskType
	if err := fetchVFXFilesKitsuJSON(ctx, connection, token, "/data/task-types", &catalog); err != nil {
		return err
	}
	if len(catalog) > vfxSnapshotMaxItems {
		return errors.New("Kitsu Task Type catalog exceeds snapshot bounds")
	}
	resolved := make(map[string]struct{}, len(needed))
	for _, taskType := range catalog {
		id := strings.ToLower(strings.TrimSpace(taskType.ID))
		if _, required := needed[id]; !required {
			continue
		}
		if !vfxfiles.ValidID(id) {
			return errors.New("Kitsu Task Type catalog contains an invalid identity")
		}
		if _, duplicate := resolved[id]; duplicate {
			return errors.New("Kitsu Task Type catalog contains duplicate referenced identities")
		}
		resolved[id] = struct{}{}
		*out = append(*out, taskType)
	}
	if len(resolved) != len(needed) {
		return errors.New("Kitsu Task Type metadata is unavailable for an existing Task")
	}
	return nil
}

// Project asset listings return a bare, unpaginated array. The page parameter
// is ignored by Zou's project-assets resource, so fetching subsequent pages
// would return the same rows and make a valid snapshot fail on duplicate IDs.
func fetchVFXFilesKitsuArray(ctx context.Context, connection KitsuURLModel, token, path string, out *[]vfxFilesSnapshotRawObject) error {
	var rows []vfxFilesSnapshotRawObject
	if err := fetchVFXFilesKitsuJSON(ctx, connection, token, path, &rows); err != nil {
		return err
	}
	if len(rows) > vfxSnapshotMaxItems {
		return errors.New("Kitsu asset listing exceeds snapshot bounds")
	}
	seen := make(map[string]struct{})
	for _, row := range rows {
		id := strings.ToLower(strings.TrimSpace(row.ID))
		if !vfxfiles.ValidID(id) {
			return errors.New("Kitsu asset listing contains an invalid identity")
		}
		if _, duplicate := seen[id]; duplicate {
			return errors.New("Kitsu asset listing contains duplicate identities")
		}
		seen[id] = struct{}{}
	}
	*out = rows
	return nil
}

func fetchVFXFilesKitsuTaskPages(ctx context.Context, connection KitsuURLModel, token, path string, out *[]vfxFilesSnapshotRawObject) error {
	var rows []vfxFilesSnapshotRawObject
	for page := 1; page <= vfxSnapshotMaxPages; page++ {
		query := url.Values{"page": {fmt.Sprint(page)}, "limit": {fmt.Sprint(vfxSnapshotPageSize)}}
		var response struct {
			Data   []vfxFilesSnapshotRawObject `json:"data"`
			Page   int                         `json:"page"`
			IsMore bool                        `json:"is_more"`
		}
		if err := fetchVFXFilesKitsuJSONQuery(ctx, connection, token, path, query, &response); err != nil {
			return err
		}
		if response.Page != 0 && response.Page != page {
			return errors.New("Kitsu Task page response is inconsistent")
		}
		if len(response.Data) > vfxSnapshotPageSize || len(rows)+len(response.Data) > vfxSnapshotMaxItems {
			return errors.New("Kitsu Task list exceeds snapshot bounds")
		}
		rows = append(rows, response.Data...)
		if !response.IsMore {
			*out = rows
			return nil
		}
		if len(response.Data) == 0 {
			return errors.New("Kitsu Task pagination did not advance")
		}
	}
	return errors.New("Kitsu Task page limit exceeded")
}

func uploadVFXFilesSnapshot(ctx context.Context, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID string, configRevision uint64, sourceETag string, objects []vfxFilesSnapshotObject, idempotency string) (string, error) {
	pages, hashes, snapshotHash, err := pageVFXFilesSnapshot(objects)
	if err != nil {
		return "", err
	}
	keyDigest := sha256.Sum256([]byte(strings.TrimSpace(idempotency) + "\n" + productionID + "\n" + fmt.Sprint(configRevision) + "\n" + sourceETag))
	operationKey := "ksfs-" + hex.EncodeToString(keyDigest[:])
	snapshotID := deterministicVFXFilesUUID(keyDigest[:16])
	startBody, _ := json.Marshal(map[string]any{"contract_version": "1.0", "snapshot_id": snapshotID, "config_revision": configRevision, "source_etag": sourceETag, "page_count": len(pages), "object_count": len(objects), "snapshot_sha256": snapshotHash})
	startPath := "/productions/" + productionID + "/sync-snapshots"
	started, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, startPath, nil, startBody, "vfx.sync.snapshot.write", true, operationKey+":start", "")
	if err != nil {
		return "", fmt.Errorf("Plugin snapshot session could not be started: %w", err)
	}
	if started.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("Plugin snapshot session could not be started: status %d", started.StatusCode)
	}
	for index, page := range pages {
		body, err := json.Marshal(map[string]any{"contract_version": "1.0", "page_index": index, "page_sha256": hashes[index], "objects": page})
		if err != nil {
			return "", errors.New("snapshot page could not be encoded")
		}
		path := fmt.Sprintf("/sync-snapshots/%s/pages/%d", snapshotID, index)
		uploaded, callErr := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPut, path, nil, body, "vfx.sync.snapshot.write", true, operationKey+fmt.Sprintf(":page-%d", index), "")
		if callErr != nil || uploaded.StatusCode != http.StatusOK {
			return "", errors.New("Plugin rejected a snapshot page")
		}
	}
	completeBody, _ := json.Marshal(map[string]any{"contract_version": "1.0", "page_count": len(pages), "object_count": len(objects), "snapshot_sha256": snapshotHash})
	completed, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, "/sync-snapshots/"+snapshotID+"/complete", nil, completeBody, "vfx.sync.snapshot.write", true, operationKey+":complete", "")
	if err != nil || completed.StatusCode != http.StatusOK {
		return "", errors.New("Plugin could not seal the Kitsu snapshot")
	}
	var result struct {
		ContractVersion string `json:"contract_version"`
		ID              string `json:"id"`
		ProductionID    string `json:"production_id"`
		State           string `json:"state"`
		ConfigRevision  uint64 `json:"config_revision"`
		ObjectCount     int    `json:"object_count"`
		PageCount       int    `json:"page_count"`
	}
	if json.Unmarshal(completed.Body, &result) != nil || result.ContractVersion != "1.0" || result.ID != snapshotID || result.ProductionID != productionID || result.State != "complete" || result.ConfigRevision != configRevision || result.ObjectCount != len(objects) || result.PageCount != len(pages) {
		return "", errors.New("Plugin snapshot completion response did not match the upload")
	}
	return snapshotID, nil
}

func deterministicVFXFilesUUID(value []byte) string {
	if len(value) != 16 {
		return ""
	}
	id := append([]byte(nil), value...)
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}
