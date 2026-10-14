package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const contractDir = "../../docs/integrations/vfx-files-plugin/v1"

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(contractDir, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return value
}

func TestVFXFilesContractDocumentsAreValidAndReferencesResolve(t *testing.T) {
	var files []string
	for _, pattern := range []string{filepath.Join(contractDir, "*.json"), filepath.Join(contractDir, "schemas", "*.json"), filepath.Join(contractDir, "fixtures", "*.json")} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) < 10 {
		t.Fatalf("contract unexpectedly has only %d JSON documents", len(files))
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			checkRefs(t, filepath.Dir(path), value)
		})
	}
}

func TestVFXFilesOAuthReturnAndPreviewRetrySemanticsAreDocumented(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(contractDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"/bot/admin/vfx-files/oauth-return",
		"only `authorization_id` as a query parameter",
		"same logged-in actor",
		"stable `Idempotency-Key`",
		"the existing Preview resource",
		"explicit activation",
	} {
		if !strings.Contains(string(readme), required) {
			t.Errorf("VFX Files contract documentation missing %q", required)
		}
	}
}

func checkRefs(t *testing.T, base string, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok {
			if strings.HasPrefix(ref, "#/") {
				return
			}
			file := strings.SplitN(ref, "#", 2)[0]
			if file != "" {
				if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(file))); err != nil {
					t.Errorf("unresolved file reference %q: %v", ref, err)
				}
			}
		}
		for _, child := range typed {
			checkRefs(t, base, child)
		}
	case []any:
		for _, child := range typed {
			checkRefs(t, base, child)
		}
	}
}

func TestOpenAPIContractHasBothTrustDirectionsAndCriticalOperations(t *testing.T) {
	plugin := readJSON(t, "plugin-api.openapi.json")
	events := readJSON(t, "kitsusync-events.openapi.json")
	if plugin["openapi"] != "3.1.0" || events["openapi"] != "3.1.0" {
		t.Fatal("both directions must use OpenAPI 3.1")
	}
	pluginPaths, ok := plugin["paths"].(map[string]any)
	if !ok {
		t.Fatal("Plugin API paths are missing")
	}
	for _, path := range []string{
		"/drive-connections/authorizations",
		"/drive-connections/{connection_id}/revoke",
		"/drive-connections/{connection_id}/roots",
		"/productions/{production_id}/desired-config",
		"/productions/{production_id}/activation",
		"/productions/{production_id}/deactivation",
		"/productions/{production_id}/sync-snapshots",
		"/sync-snapshots/{snapshot_id}/pages/{page_index}",
		"/sync-snapshots/{snapshot_id}/complete",
		"/productions/{production_id}/sync-previews",
		"/productions/{production_id}/sync-jobs",
		"/sync-jobs/{job_id}/retry",
		"/sync-jobs/{job_id}/reconcile",
	} {
		if _, ok := pluginPaths[path]; !ok {
			t.Errorf("Plugin API missing %s", path)
		}
	}
	eventPaths, ok := events["paths"].(map[string]any)
	if !ok || eventPaths["/events"] == nil {
		t.Fatal("Plugin-to-KitsuSync event endpoint is missing")
	}
	eventOperation := eventPaths["/events"].(map[string]any)["post"].(map[string]any)
	for _, header := range []string{"X-VFX-Key-Id", "X-VFX-Timestamp", "X-VFX-Nonce", "X-VFX-Content-SHA256", "X-VFX-Signature", "Idempotency-Key"} {
		if !parametersContainHeader(eventOperation["parameters"], header) {
			t.Errorf("signed event intake must require %s", header)
		}
	}
	eventBatchSchema := readJSON(t, "schemas/event-batch.schema.json")
	if !stringSliceContains(eventBatchSchema["required"], "events") {
		t.Fatal("event batch schema must require a nonempty event list")
	}
	connectionSchema := plugin["components"].(map[string]any)["schemas"].(map[string]any)["DriveConnection"].(map[string]any)
	if propertyListContains(connectionSchema["properties"], "credential_ref", "access_token", "refresh_token", "webhook_url", "secret") {
		t.Fatal("public connection response exposes credential material or internal references")
	}
	previewRequest := pluginPaths["/productions/{production_id}/sync-previews"].(map[string]any)["post"].(map[string]any)["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if previewRequest["$ref"] == nil || !strings.HasSuffix(previewRequest["$ref"].(string), "SyncPreviewRequest") {
		t.Fatal("sync preview must bind to a named request schema")
	}
	previewSchema := plugin["components"].(map[string]any)["schemas"].(map[string]any)["SyncPreviewRequest"].(map[string]any)
	if !stringSliceContains(previewSchema["required"], "snapshot_id") {
		t.Fatal("sync preview must require a completed KitsuSync-owned snapshot")
	}
	update := readJSON(t, "schemas/desired-config-update.schema.json")
	if propertyListContains(update["properties"], "activation") {
		t.Fatal("ordinary desired-config update must not bypass the explicit activation endpoint")
	}
	desiredConfigPut := pluginPaths["/productions/{production_id}/desired-config"].(map[string]any)["put"].(map[string]any)
	if !parametersContainOptionalHeader(desiredConfigPut["parameters"], "If-Match") || !parametersContainOptionalHeader(desiredConfigPut["parameters"], "If-None-Match") {
		t.Fatal("desired-config PUT must support conditional creation and conditional updates")
	}
	responses := desiredConfigPut["responses"].(map[string]any)
	if responses["201"] == nil || responses["200"] == nil || responses["412"] == nil {
		t.Fatal("desired-config PUT must document create, update, and failed precondition responses")
	}
	createdResponse := responses["201"].(map[string]any)
	createdContent := createdResponse["content"].(map[string]any)
	createdJSON := createdContent["application/json"].(map[string]any)
	createdSchema := createdJSON["schema"].(map[string]any)
	if createdSchema["$ref"] != "schemas/desired-production-config.schema.json" {
		t.Fatalf("conditional create must return the desired config, got response schema %#v", createdSchema)
	}
	desiredConfig := readJSON(t, "schemas/desired-production-config.schema.json")
	driveSchema := desiredConfig["properties"].(map[string]any)["drive"].(map[string]any)
	if !driveModeRequires(driveSchema, "my_drive", "owner_subject") || !driveModeRequires(driveSchema, "shared_drive", "drive_id") {
		t.Fatal("desired config schema must require the Plugin-verified My Drive subject or Shared Drive identity")
	}
	if !strings.Contains(desiredConfigPut["description"].(string), "exactly one strong If-Match") {
		t.Fatal("desired-config conditional create/update exclusivity is not documented")
	}
	verification := plugin["components"].(map[string]any)["schemas"].(map[string]any)["RootVerification"].(map[string]any)
	if !stringSliceContains(verification["required"], "account_mode") || !stringSliceContains(verification["required"], "owner_subject") {
		t.Fatal("root verification must identify the verified Drive account subject for initial My Drive configuration")
	}
	if !propertyListContains(verification["properties"], "owner_subject") {
		t.Fatal("root verification must expose the verified account subject, never a client-supplied identity")
	}
	for _, path := range []string{"/productions/{production_id}/activation", "/productions/{production_id}/deactivation"} {
		operation := pluginPaths[path].(map[string]any)["post"].(map[string]any)
		if !parametersContainHeader(operation["parameters"], "If-Match") {
			t.Errorf("%s must use a revision precondition", path)
		}
		for _, raw := range operation["parameters"].([]any) {
			parameter, ok := raw.(map[string]any)
			if ok && parameter["name"] == "If-Match" {
				schema := parameter["schema"].(map[string]any)
				if schema["type"] != "string" || schema["pattern"] != `^"[1-9][0-9]*"$` {
					t.Errorf("%s must use a strong quoted numeric ETag", path)
				}
			}
		}
	}
	for path, methods := range pluginPaths {
		for method, rawOperation := range methods.(map[string]any) {
			operation := rawOperation.(map[string]any)
			if !parametersContainHeader(operation["parameters"], "X-KitsuSync-Actor-ID") {
				t.Errorf("Plugin operation %s %s must carry the server-derived actor identity", method, path)
			}
			if method != "post" && method != "put" && method != "patch" && method != "delete" {
				continue
			}
			if !parametersContainHeader(operation["parameters"], "Idempotency-Key") {
				t.Errorf("stateful Plugin operation %s %s must require Idempotency-Key", method, path)
			}
		}
	}
	for _, path := range []string{"/drive-connections", "/drive-connections/{connection_id}", "/drive-connections/{connection_id}/roots"} {
		operation := pluginPaths[path].(map[string]any)["get"].(map[string]any)
		if !parametersContainQuery(operation["parameters"], "production_id") {
			t.Errorf("%s must scope connection/root reads to a Production", path)
		}
	}
}

func driveModeRequires(schema map[string]any, mode, requiredProperty string) bool {
	conditions, ok := schema["allOf"].([]any)
	if !ok {
		return false
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ifClause, ok := condition["if"].(map[string]any)
		if !ok {
			continue
		}
		properties, ok := ifClause["properties"].(map[string]any)
		if !ok {
			continue
		}
		accountMode, ok := properties["account_mode"].(map[string]any)
		if !ok || accountMode["const"] != mode {
			continue
		}
		thenClause, ok := condition["then"].(map[string]any)
		if !ok {
			return false
		}
		return stringSliceContains(thenClause["required"], requiredProperty)
	}
	return false
}

func TestMyDriveOwnerSubjectFixtureComesFromVerifiedPluginResponse(t *testing.T) {
	fixture := readJSON(t, "fixtures/root-verification-my-drive.json")
	if fixture["account_mode"] != "my_drive" || fixture["state"] != "verified" || fixture["owner_subject"] != "synthetic-oidc-subject-01" || fixture["connection_id"] == nil || fixture["root_id"] == nil {
		t.Fatal("My Drive fixture must carry an opaque server-verified identity bound to connection and root")
	}
	if containsForbiddenKey(fixture) {
		t.Fatal("root verification fixture contains credential material")
	}
}

func parametersContainOptionalHeader(value any, wanted string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range items {
		parameter, ok := raw.(map[string]any)
		if ok && parameter["in"] == "header" && parameter["name"] == wanted {
			return parameter["required"] == false
		}
	}
	return false
}

func TestOpenAPIRequiredMembersAreStringsBackedByProperties(t *testing.T) {
	plugin := readJSON(t, "plugin-api.openapi.json")
	components := plugin["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	for name, raw := range schemas {
		schema := raw.(map[string]any)
		required, ok := schema["required"].([]any)
		if !ok {
			continue
		}
		properties, _ := schema["properties"].(map[string]any)
		for _, value := range required {
			field, ok := value.(string)
			if !ok || field == "" {
				t.Errorf("schema %s has a non-string/empty required member: %#v", name, value)
				continue
			}
			if _, exists := properties[field]; !exists {
				t.Errorf("schema %s requires %q but does not define it in properties", name, field)
			}
		}
	}
	operatorCommand := schemas["OperatorCommand"].(map[string]any)
	if !stringSliceContains(operatorCommand["required"], "contract_version") || len(operatorCommand["required"].([]any)) != 1 {
		t.Fatalf("OperatorCommand must require only its defined contract_version field: %#v", operatorCommand["required"])
	}
}

func TestServiceBearerProfileAndZouRouteMappingAreExplicit(t *testing.T) {
	plugin := readJSON(t, "plugin-api.openapi.json")
	profile := plugin["x-service-bearer-profile"].(map[string]any)
	if profile["standard"] != "RFC 9068 JWT access token profile" || profile["type"] != "at+JWT" || profile["algorithm"] != "RS256" {
		t.Fatalf("unexpected service-bearer standard: %#v", profile)
	}
	serverURL := plugin["servers"].([]any)[0].(map[string]any)["url"].(string)
	if !strings.HasSuffix(serverURL, "/api/plugins/vfx_files/integrations/v1") || strings.Contains(serverURL, "/api/integrations/v1") {
		t.Fatalf("management API must use Zou's official vfx_files plugin route prefix: %s", serverURL)
	}
	permitted, ok := profile["permitted_scopes"].([]any)
	if !ok || len(permitted) == 0 {
		t.Fatal("service bearer profile must publish its allowed operation scopes")
	}
	allowed := make(map[string]bool, len(permitted))
	for _, raw := range permitted {
		name, ok := raw.(string)
		if !ok || name == "" || allowed[name] {
			t.Fatalf("invalid or duplicate allowed scope: %#v", raw)
		}
		allowed[name] = true
	}
	paths := plugin["paths"].(map[string]any)
	for path, rawMethods := range paths {
		for method, rawOperation := range rawMethods.(map[string]any) {
			operation := rawOperation.(map[string]any)
			scope, ok := operation["x-required-scope"].(string)
			if !ok || !allowed[scope] {
				t.Errorf("operation %s %s must declare an allowed required service scope", method, path)
			}
		}
	}
	if profile["maximum_lifetime_seconds"] != float64(300) || profile["clock_skew_seconds"] != float64(30) || profile["key_discovery_path"] != "/.well-known/vfx-files-jwks.json" {
		t.Fatalf("service token expiry or key-discovery policy is incomplete: %#v", profile)
	}
	events := readJSON(t, "kitsusync-events.openapi.json")
	eventServer := events["servers"].([]any)[0].(map[string]any)["url"].(string)
	if !strings.Contains(eventServer, "/api/integrations/vfx-files/v1") || strings.Contains(eventServer, "/api/plugins/") {
		t.Fatalf("Plugin signed event intake must remain a separate KitsuSync service route: %s", eventServer)
	}
}

func TestTemplateCatalogMakesProductionRootAndFolderMaterializationExplicit(t *testing.T) {
	plugin := readJSON(t, "plugin-api.openapi.json")
	schemas := plugin["components"].(map[string]any)["schemas"].(map[string]any)
	list := schemas["TemplateList"].(map[string]any)
	item := list["properties"].(map[string]any)["templates"].(map[string]any)["items"].(map[string]any)
	if !stringSliceContains(item["required"], "materialization") {
		t.Fatal("every catalog selection must state how the Plugin materializes it")
	}
	materialization := item["properties"].(map[string]any)["materialization"].(map[string]any)
	if !stringSliceContains(materialization["required"], "schema_revision") ||
		!stringSliceContains(materialization["required"], "root_binding") ||
		!stringSliceContains(materialization["required"], "available_overrides") {
		t.Fatal("materialization must declare schema revision, root interpretation and allowed override keys")
	}
	properties := materialization["properties"].(map[string]any)
	if properties["schema_revision"].(map[string]any)["const"] != float64(1) {
		t.Fatal("only the reviewed materialization schema revision is accepted")
	}
	if properties["root_binding"].(map[string]any)["const"] != "verified_root_is_production_root" {
		t.Fatal("the v1 selected-root interpretation must be explicit")
	}
	patternDescription := item["properties"].(map[string]any)["pattern"].(map[string]any)["description"].(string)
	if !strings.Contains(patternDescription, "does not create a child folder") {
		t.Fatal("Production catalog pattern must not imply creating a duplicate named Production folder")
	}
}

func TestDriveFolderURLCandidateIsStrictlyAllowlisted(t *testing.T) {
	plugin := readJSON(t, "plugin-api.openapi.json")
	request := plugin["components"].(map[string]any)["schemas"].(map[string]any)["RootVerificationRequest"].(map[string]any)
	urlPattern := request["properties"].(map[string]any)["candidate_url"].(map[string]any)["pattern"].(string)
	pattern, err := regexp.Compile(urlPattern)
	if err != nil {
		t.Fatalf("candidate URL allowlist pattern must compile: %v", err)
	}
	for _, value := range []string{
		"https://drive.google.com/drive/folders/fake-root-01",
		"https://drive.google.com/drive/u/0/folders/fake-root-01?usp=drive_link",
	} {
		if !pattern.MatchString(value) {
			t.Errorf("expected allowlisted Drive folder URL to match: %s", value)
		}
	}
	for _, value := range []string{
		"http://drive.google.com/drive/folders/fake-root-01",
		"https://drive.google.com.evil.invalid/drive/folders/fake-root-01",
		"https://user@drive.google.com/drive/folders/fake-root-01",
		"https://drive.google.com:8443/drive/folders/fake-root-01",
		"https://drive.google.com/drive/file/d/fake-root-01",
	} {
		if pattern.MatchString(value) {
			t.Errorf("unexpected URL accepted as Drive folder candidate: %s", value)
		}
	}
}

func stringSliceContains(value any, wanted string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

func parametersContainHeader(value any, wanted string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range items {
		parameter, ok := raw.(map[string]any)
		if ok && parameter["in"] == "header" && parameter["name"] == wanted && parameter["required"] == true {
			return true
		}
	}
	return false
}

func parametersContainQuery(value any, wanted string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range items {
		parameter, ok := raw.(map[string]any)
		if ok && parameter["in"] == "query" && parameter["name"] == wanted && parameter["required"] == true {
			return true
		}
	}
	return false
}

func propertyListContains(value any, names ...string) bool {
	properties, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, name := range names {
		if _, found := properties[name]; found {
			return true
		}
	}
	return false
}

type eventBatch struct {
	ContractVersion string      `json:"contract_version"`
	Events          []fileEvent `json:"events"`
}

type fileEvent struct {
	ContractVersion string         `json:"contract_version"`
	ProducerID      string         `json:"producer_id"`
	EventID         string         `json:"event_id"`
	EventType       string         `json:"event_type"`
	OccurredAt      string         `json:"occurred_at"`
	ProductionID    string         `json:"production_id"`
	ActivationEpoch int            `json:"activation_epoch"`
	Source          taskIdentity   `json:"source"`
	OutputPackage   outputPackage  `json:"output_package"`
	Delivery        deliveryTarget `json:"delivery"`
}

type taskIdentity struct {
	ID         string `json:"id"`
	TaskTypeID string `json:"task_type_id"`
}

type outputPackage struct {
	ID             string `json:"id"`
	Version        int    `json:"version"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

type deliveryTarget struct {
	ID                  string `json:"id"`
	Revision            int    `json:"revision"`
	Status              string `json:"status"`
	RecipientTaskID     string `json:"recipient_task_id"`
	RecipientTaskTypeID string `json:"recipient_task_type_id"`
}

func TestMultiRecipientFixtureHasIndependentStableDeliveries(t *testing.T) {
	var batch eventBatch
	data, err := os.ReadFile(filepath.Join(contractDir, "fixtures", "delivery-created-multi-target.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &batch); err != nil {
		t.Fatal(err)
	}
	if batch.ContractVersion != "1.0" || len(batch.Events) != 2 {
		t.Fatalf("unexpected fixture envelope: version=%q events=%d", batch.ContractVersion, len(batch.Events))
	}
	seenEvent, seenDelivery, seenRecipient := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, event := range batch.Events {
		if event.ContractVersion != "1.0" || event.ProducerID != "vfx-files-plugin" || event.EventType != "file_delivery.created" || event.ProductionID == "" || event.ActivationEpoch < 1 {
			t.Fatalf("invalid file event identity: %#v", event)
		}
		if _, err := time.Parse(time.RFC3339, event.OccurredAt); err != nil {
			t.Errorf("invalid event timestamp: %v", err)
		}
		if event.EventID == "" || event.Delivery.ID == "" || event.Delivery.Revision < 1 || event.Delivery.Status != "created" || event.Delivery.RecipientTaskID == "" || event.Delivery.RecipientTaskTypeID == "" {
			t.Fatalf("incomplete recipient delivery identity: %#v", event.Delivery)
		}
		if seenEvent[event.EventID] || seenDelivery[event.Delivery.ID] || seenRecipient[event.Delivery.RecipientTaskID] {
			t.Fatal("multi-recipient deliveries must have distinct event, delivery, and recipient identities")
		}
		seenEvent[event.EventID], seenDelivery[event.Delivery.ID], seenRecipient[event.Delivery.RecipientTaskID] = true, true, true
	}
}

func TestFixturesHaveSafeStateAndContainNoCredentialFields(t *testing.T) {
	config := readJSON(t, "fixtures/desired-config-adopt-existing.json")
	if config["contract_version"] != "1.0" || config["production_id"] == nil || config["config_revision"] != float64(4) {
		t.Fatal("desired-config fixture is missing stable scope or revision")
	}
	activation := config["activation"].(map[string]any)
	if activation["enabled"] != false || activation["notification_epoch"] != float64(0) {
		t.Fatal("fake desired config must start inactive and suppress bootstrap notifications")
	}
	for _, name := range []string{"delivery-created-multi-target.json", "delivery-needs-fix.json", "delivery-revised.json", "folder-provisioning-completed.json", "output-package-created.json", "output-package-ready.json"} {
		fixture := readJSON(t, "fixtures/"+name)
		if containsForbiddenKey(fixture) {
			t.Errorf("fixture %s contains a credential or live endpoint field", name)
		}
		if events, ok := fixture["events"].([]any); ok {
			for _, raw := range events {
				event := raw.(map[string]any)
				if event["activation_epoch"] != float64(1) {
					t.Errorf("fixture %s must bind each event to an explicit post-activation epoch", name)
				}
				if _, exists := event["bootstrap_import"]; exists {
					t.Errorf("fixture %s must not rely on a producer-controlled bootstrap suppression flag", name)
				}
			}
		}
	}
}

func TestSnapshotFixturesHaveStableIdentityAndMatchingCanonicalDigests(t *testing.T) {
	start := readJSON(t, "fixtures/sync-snapshot-start.json")
	page := readJSON(t, "fixtures/sync-snapshot-page-0.json")
	if start["contract_version"] != "1.0" || page["contract_version"] != "1.0" || start["snapshot_id"] == nil {
		t.Fatal("snapshot fixtures must include contract and stable identity")
	}
	if start["snapshot_sha256"] != page["page_sha256"] {
		t.Fatal("single-page snapshot digest must equal its page digest")
	}
	objects, ok := page["objects"].([]any)
	if !ok || len(objects) != int(start["object_count"].(float64)) {
		t.Fatal("snapshot page count does not match declared object count")
	}
	canonicalBytes, err := json.Marshal(objects)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonicalBytes)
	if hex.EncodeToString(digest[:]) != page["page_sha256"] {
		t.Fatal("snapshot fixture page hash does not match its canonical JSON objects")
	}
	seen := make(map[string]bool)
	objectsByID := make(map[string]map[string]any)
	parents := make([]string, 0, len(objects))
	for _, raw := range objects {
		object := raw.(map[string]any)
		id, typ := object["id"].(string), object["object_type"].(string)
		if id == "" || typ == "" || seen[id] {
			t.Fatalf("snapshot IDs must be non-empty and unique: %#v", object)
		}
		seen[id] = true
		objectsByID[id] = object
		if _, ok := object["included_in_sync"].(bool); !ok {
			t.Fatalf("snapshot must distinguish selected objects from hierarchy-only context: %#v", object)
		}
		if parentID, ok := object["parent_id"].(string); ok && parentID != "" {
			parents = append(parents, parentID)
		}
	}
	for _, raw := range objects {
		object := raw.(map[string]any)
		if object["object_type"] == "task" {
			taskTypeID, ok := object["task_type_id"].(string)
			if !ok || objectsByID[taskTypeID] == nil || objectsByID[taskTypeID]["object_type"] != "task_type" {
				t.Errorf("task type identity must resolve to a Task Type snapshot: %#v", object)
			}
		}
	}
	for _, parentID := range parents {
		found := false
		for _, raw := range objects {
			if raw.(map[string]any)["id"] == parentID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("snapshot hierarchy parent %s is missing", parentID)
		}
	}
}

func containsForbiddenKey(value any) bool {
	forbidden := map[string]bool{"access_token": true, "refresh_token": true, "client_secret": true, "authorization_code": true, "credential_ref": true, "webhook_url": true, "secret": true, "cookie": true, "session": true}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if forbidden[strings.ToLower(key)] || containsForbiddenKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsForbiddenKey(child) {
				return true
			}
		}
	}
	return false
}

func TestDeliveryTransitionFixturesKeepPerRecipientRevision(t *testing.T) {
	for _, tc := range []struct {
		file string
		want string
	}{{"delivery-needs-fix.json", "file_delivery.needs_fix"}, {"delivery-revised.json", "file_delivery.revised"}} {
		t.Run(tc.file, func(t *testing.T) {
			fixture := readJSON(t, "fixtures/"+tc.file)
			batch := eventBatch{}
			data, _ := json.Marshal(fixture)
			if err := json.Unmarshal(data, &batch); err != nil {
				t.Fatal(err)
			}
			if len(batch.Events) != 1 || batch.Events[0].EventType != tc.want || batch.Events[0].Delivery.Revision <= 1 {
				t.Fatalf("transition must retain target identity and advance its revision: %#v", batch.Events)
			}
		})
	}
}
