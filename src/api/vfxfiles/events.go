package vfxfiles

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"app/src/model"
	"gorm.io/gorm"
)

const maxEventBatchBytes = 1 << 20

var eventSignatureRE = regexp.MustCompile(`^sha256=[a-f0-9]{64}$`)

type eventBatch struct {
	ContractVersion string            `json:"contract_version"`
	Events          []json.RawMessage `json:"events"`
}
type pluginEvent struct {
	ContractVersion string              `json:"contract_version"`
	ProducerID      string              `json:"producer_id"`
	EventID         string              `json:"event_id"`
	EventType       string              `json:"event_type"`
	OccurredAt      string              `json:"occurred_at"`
	ProductionID    string              `json:"production_id"`
	ActivationEpoch uint64              `json:"activation_epoch"`
	ProductionName  string              `json:"production_name,omitempty"`
	EpisodeID       *string             `json:"episode_id,omitempty"`
	EpisodeName     *string             `json:"episode_name,omitempty"`
	Entity          *eventEntity        `json:"entity,omitempty"`
	Source          *eventTask          `json:"source,omitempty"`
	Sender          *eventPerson        `json:"sender,omitempty"`
	SenderPersonID  *string             `json:"sender_person_id,omitempty"`
	OutputPackage   *eventOutputPackage `json:"output_package,omitempty"`
	Delivery        *eventDelivery      `json:"delivery,omitempty"`
	Folder          *eventFolder        `json:"folder,omitempty"`
}
type eventEntity struct {
	Type string  `json:"type"`
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Code *string `json:"code,omitempty"`
}
type eventTask struct {
	ID           string `json:"id,omitempty"`
	TaskID       string `json:"task_id,omitempty"`
	TaskTypeID   string `json:"task_type_id"`
	TaskTypeName string `json:"task_type_name,omitempty"`
}
type eventPerson struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}
type eventOutputPackage struct {
	ID             string           `json:"id"`
	Version        int              `json:"version"`
	Classification string           `json:"classification"`
	ManifestSHA256 string           `json:"manifest_sha256"`
	ManifestRef    *string          `json:"manifest_ref,omitempty"`
	Objects        []eventObject    `json:"objects"`
	FrameRange     *eventFrameRange `json:"frame_range,omitempty"`
}
type eventObject struct {
	DriveFileID    string `json:"drive_file_id"`
	Classification string `json:"classification"`
	SizeBytes      int64  `json:"size_bytes"`
	SHA256         string `json:"sha256"`
}
type eventFrameRange struct {
	Start   int `json:"start"`
	End     int `json:"end"`
	Step    int `json:"step"`
	RateNum int `json:"rate_num"`
	RateDen int `json:"rate_den"`
}
type eventDelivery struct {
	ID                    string  `json:"id"`
	Revision              uint64  `json:"revision"`
	Status                string  `json:"status"`
	RecipientTaskID       string  `json:"recipient_task_id"`
	RecipientTaskTypeID   string  `json:"recipient_task_type_id"`
	RecipientTaskTypeName string  `json:"recipient_task_type_name"`
	FeedbackCode          *string `json:"feedback_code,omitempty"`
}
type eventFolder struct {
	ObjectType   string `json:"object_type"`
	ObjectID     string `json:"object_id"`
	DriveFileID  string `json:"drive_file_id"`
	BindingState string `json:"binding_state"`
}

// FileDeliveryNotification is the bounded display subset consumed by the
// existing Discord delivery path. It intentionally excludes Drive URLs and
// object IDs; the Plugin contract does not grant public sharing access.
type FileDeliveryNotification struct {
	EventID, EventType, ProductionID, ProductionName, OccurredAt            string
	EntityType, EntityID, EntityName                                        string
	SourceTaskID, SourceTaskTypeID, SourceTaskTypeName                      string
	SenderName, OutputClassification                                        string
	OutputVersion                                                           int
	DeliveryID, RecipientTaskID, RecipientTaskTypeID, RecipientTaskTypeName string
}

// DecodeFileDeliveryNotification validates a durable inbox payload before it
// is used to build a user-visible notification.
func DecodeFileDeliveryNotification(raw []byte) (FileDeliveryNotification, error) {
	input, err := parsePluginEvent(raw)
	if err != nil || !strings.HasPrefix(input.EventType, "file_delivery.") {
		return FileDeliveryNotification{}, errors.New("invalid file delivery event")
	}
	var event pluginEvent
	if err := json.Unmarshal(raw, &event); err != nil || event.Entity == nil || event.Source == nil || event.Sender == nil || event.OutputPackage == nil || event.Delivery == nil {
		return FileDeliveryNotification{}, errors.New("invalid file delivery event")
	}
	return FileDeliveryNotification{
		EventID: event.EventID, EventType: event.EventType, ProductionID: event.ProductionID, ProductionName: event.ProductionName, OccurredAt: event.OccurredAt,
		EntityType: event.Entity.Type, EntityID: event.Entity.ID, EntityName: event.Entity.Name,
		SourceTaskID: event.Source.ID, SourceTaskTypeID: event.Source.TaskTypeID, SourceTaskTypeName: event.Source.TaskTypeName,
		SenderName: event.Sender.DisplayName, OutputClassification: event.OutputPackage.Classification, OutputVersion: event.OutputPackage.Version,
		DeliveryID: event.Delivery.ID, RecipientTaskID: event.Delivery.RecipientTaskID, RecipientTaskTypeID: event.Delivery.RecipientTaskTypeID,
		RecipientTaskTypeName: event.Delivery.RecipientTaskTypeName,
	}, nil
}

// EventHandler verifies the exact signed raw body and durably records nonce and
// event identity before returning 202. The supplied keys are server-only and
// are bound to the sole contract producer (vfx-files-plugin).
func EventHandler(db *gorm.DB, keys map[string][]byte, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if db == nil || len(keys) == 0 {
			http.Error(w, "integration unavailable", http.StatusServiceUnavailable)
			return
		}
		if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
			http.Error(w, "invalid content type", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxEventBatchBytes+1))
		if err != nil || len(body) == 0 || len(body) > maxEventBatchBytes {
			http.Error(w, "invalid event batch", http.StatusRequestEntityTooLarge)
			return
		}
		keyID := strings.TrimSpace(r.Header.Get("X-VFX-Key-Id"))
		timestamp := strings.TrimSpace(r.Header.Get("X-VFX-Timestamp"))
		nonce := strings.TrimSpace(r.Header.Get("X-VFX-Nonce"))
		suppliedHash := strings.TrimSpace(r.Header.Get("X-VFX-Content-SHA256"))
		signature := strings.TrimSpace(r.Header.Get("X-VFX-Signature"))
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		key := keys[keyID]
		if !keyIDRE.MatchString(keyID) || len(key) < 32 || len(nonce) < 16 || len(nonce) > 128 || strings.ContainsAny(nonce, "\r\n") || len(idempotencyKey) < 16 || len(idempotencyKey) > 128 || strings.ContainsAny(idempotencyKey, "\r\n") || !eventSignatureRE.MatchString(signature) {
			http.Error(w, "invalid event authentication", http.StatusUnauthorized)
			return
		}
		parsedTime, err := time.Parse(time.RFC3339, timestamp)
		current := now().UTC()
		if err != nil || parsedTime.Before(current.Add(-5*time.Minute)) || parsedTime.After(current.Add(5*time.Minute)) {
			http.Error(w, "invalid event timestamp", http.StatusUnauthorized)
			return
		}
		sum := sha256.Sum256(body)
		bodyHash := hex.EncodeToString(sum[:])
		if suppliedHash != bodyHash {
			http.Error(w, "event body hash mismatch", http.StatusUnauthorized)
			return
		}
		canonical := timestamp + "\n" + nonce + "\n" + bodyHash + "\n" + idempotencyKey
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(canonical))
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(signature)) {
			http.Error(w, "invalid event signature", http.StatusUnauthorized)
			return
		}
		var batch eventBatch
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&batch); err != nil || batch.ContractVersion != contractVersion || len(batch.Events) == 0 || len(batch.Events) > 100 {
			http.Error(w, "invalid event batch", http.StatusBadRequest)
			return
		}
		if err := ensureSingleJSONValue(decoder); err != nil {
			http.Error(w, "invalid event batch", http.StatusBadRequest)
			return
		}
		inputs := make([]model.VFXFilesEventInput, 0, len(batch.Events))
		for _, raw := range batch.Events {
			input, err := parsePluginEvent(raw)
			if err != nil {
				http.Error(w, "invalid event payload", http.StatusBadRequest)
				return
			}
			inputs = append(inputs, input)
		}
		results, err := model.AcceptVFXFilesEvents(db, keyID, nonce, inputs, current)
		if errors.Is(err, model.ErrVFXFilesEventConflict) {
			http.Error(w, "event replay or identity conflict", http.StatusConflict)
			return
		}
		if errors.Is(err, model.ErrVFXFilesActivationPending) {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "activation state is synchronizing", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			http.Error(w, "event could not be durably accepted", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(struct {
			ContractVersion string                      `json:"contract_version"`
			Results         []model.VFXFilesEventResult `json:"results"`
		}{ContractVersion: contractVersion, Results: results})
	}
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func parsePluginEvent(raw []byte) (model.VFXFilesEventInput, error) {
	var event pluginEvent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return model.VFXFilesEventInput{}, err
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return model.VFXFilesEventInput{}, err
	}
	if event.ContractVersion != contractVersion || event.ProducerID != "vfx-files-plugin" || !validID(event.EventID) || !validID(event.ProductionID) || event.ActivationEpoch == 0 {
		return model.VFXFilesEventInput{}, errors.New("invalid event identity")
	}
	if _, err := time.Parse(time.RFC3339, event.OccurredAt); err != nil {
		return model.VFXFilesEventInput{}, errors.New("invalid event timestamp")
	}
	input := model.VFXFilesEventInput{ProducerID: event.ProducerID, EventID: event.EventID, EventType: event.EventType, ProductionID: event.ProductionID, ActivationEpoch: event.ActivationEpoch, Payload: append([]byte(nil), raw...)}
	switch event.EventType {
	case "file_delivery.created", "file_delivery.ready", "file_delivery.accepted", "file_delivery.needs_fix", "file_delivery.revised":
		if event.Entity == nil || event.Source == nil || event.Sender == nil || event.OutputPackage == nil || event.Delivery == nil || event.Folder != nil || event.SenderPersonID != nil || !validEntity(*event.Entity) || !validTask(*event.Source, true) || !validPerson(*event.Sender) || !validOutputPackage(*event.OutputPackage) || !validDelivery(*event.Delivery, event.EventType) {
			return model.VFXFilesEventInput{}, errors.New("incomplete file delivery event")
		}
		input.DeliveryID, input.DeliveryRevision = event.Delivery.ID, event.Delivery.Revision
		input.RecipientTaskID, input.RecipientTaskTypeID = event.Delivery.RecipientTaskID, event.Delivery.RecipientTaskTypeID
	case "output_package.created", "output_package.ready":
		if event.Source == nil || !validTask(*event.Source, false) || event.OutputPackage == nil || !validOutputPackage(*event.OutputPackage) || event.Delivery != nil || event.Entity != nil || event.Sender != nil || event.Folder != nil || (event.SenderPersonID != nil && !validID(*event.SenderPersonID)) {
			return model.VFXFilesEventInput{}, errors.New("incomplete output package event")
		}
	case "folder.provisioning.completed":
		if event.Folder == nil || !validFolder(*event.Folder) || event.Delivery != nil || event.Entity != nil || event.Source != nil || event.Sender != nil || event.SenderPersonID != nil || event.OutputPackage != nil {
			return model.VFXFilesEventInput{}, errors.New("incomplete folder event")
		}
	default:
		return model.VFXFilesEventInput{}, errors.New("unsupported event type")
	}
	return input, nil
}

var regexpSHA256 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var driveIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

func boundedText(value string, max int) bool {
	return len([]rune(strings.TrimSpace(value))) > 0 && len([]rune(value)) <= max && !strings.ContainsRune(value, '\x00')
}

func validEntity(entity eventEntity) bool {
	return (entity.Type == "sequence" || entity.Type == "shot" || entity.Type == "asset") && validID(entity.ID) && boundedText(entity.Name, 240) && (entity.Code == nil || len([]rune(*entity.Code)) <= 120 && !strings.ContainsRune(*entity.Code, '\x00'))
}

func validTask(task eventTask, delivery bool) bool {
	if !validID(task.TaskTypeID) || len([]rune(task.TaskTypeName)) > 160 {
		return false
	}
	if delivery {
		return validID(task.ID) && task.TaskID == "" && boundedText(task.TaskTypeName, 160)
	}
	return validID(task.TaskID) && task.ID == ""
}

func validPerson(person eventPerson) bool {
	return validID(person.ID) && boundedText(person.DisplayName, 160)
}

func validOutputPackage(pkg eventOutputPackage) bool {
	if !validID(pkg.ID) || pkg.Version < 1 || !boundedText(pkg.Classification, 100) || !regexpSHA256.MatchString(pkg.ManifestSHA256) || len(pkg.Objects) > 100 {
		return false
	}
	if pkg.ManifestRef != nil && (len([]rune(*pkg.ManifestRef)) > 200 || !regexp.MustCompile(`^manifest:[0-9a-fA-F-]{36}$`).MatchString(*pkg.ManifestRef)) {
		return false
	}
	for _, object := range pkg.Objects {
		if !driveIDRE.MatchString(object.DriveFileID) || !boundedText(object.Classification, 100) || object.SizeBytes < 0 || !regexpSHA256.MatchString(object.SHA256) {
			return false
		}
	}
	if pkg.FrameRange != nil {
		f := pkg.FrameRange
		if f.Step < 1 || f.RateNum < 1 || f.RateDen < 1 || f.End < f.Start {
			return false
		}
	}
	return true
}

func validDelivery(delivery eventDelivery, eventType string) bool {
	wantStatus := strings.TrimPrefix(eventType, "file_delivery.")
	if !validID(delivery.ID) || delivery.Revision == 0 || delivery.Status != wantStatus || !validID(delivery.RecipientTaskID) || !validID(delivery.RecipientTaskTypeID) || !boundedText(delivery.RecipientTaskTypeName, 160) {
		return false
	}
	return delivery.FeedbackCode == nil || len([]rune(*delivery.FeedbackCode)) <= 80 && !strings.ContainsRune(*delivery.FeedbackCode, '\x00')
}

func validFolder(folder eventFolder) bool {
	allowedType := map[string]bool{"production": true, "episode": true, "sequence": true, "shot": true, "asset": true, "task": true}
	return allowedType[folder.ObjectType] && validID(folder.ObjectID) && driveIDRE.MatchString(folder.DriveFileID) && folder.BindingState == "ready"
}
