package setup

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"app/src/utils/basicauth"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type runtimeBotReplacementPerson struct {
	ID        string `json:"id,omitempty"`
	Email     string `json:"email,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Role      string `json:"role,omitempty"`
	Password  string `json:"password,omitempty"`
	Active    bool   `json:"active,omitempty"`
	Archived  bool   `json:"archived,omitempty"`
	IsBot     bool   `json:"is_bot,omitempty"`
}

func verifyReplacementPasswordHash(host, token, personID, password string) error {
	var people []runtimeBotReplacementPerson
	requestURL := normalizeKitsuHostname(host) + "api/data/persons?with_pass_hash=true"
	if err := kitsuJSON(token, http.MethodGet, requestURL, nil, &people); err != nil {
		return fmt.Errorf("password hash lookup failed: %w", err)
	}
	matched := 0
	var hash string
	for _, person := range people {
		if person.ID == personID {
			matched++
			hash = person.Password
		}
	}
	if matched != 1 {
		return fmt.Errorf("password hash lookup found %d matching people", matched)
	}
	if hash == "" {
		return errors.New("password hash was not returned")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return errors.New("bcrypt verification failed")
	}
	return nil
}

func listRuntimeBotPeople(host, token string) ([]runtimeBotReplacementPerson, error) {
	var people []runtimeBotReplacementPerson
	if err := kitsuJSON(token, http.MethodGet, normalizeKitsuHostname(host)+"api/data/persons", nil, &people); err != nil {
		return nil, err
	}
	return people, nil
}

func PrepareRuntimeBotReplacement(db *gorm.DB, host, adminEmail, adminPassword, tempEmail, tempPassword string) (string, error) {
	if db == nil || strings.TrimSpace(tempEmail) == "" || strings.TrimSpace(tempPassword) == "" {
		return "", errors.New("replacement inputs are incomplete")
	}
	adminToken := basicauth.AuthForJWTToken(normalizeKitsuHostname(host)+"api/auth/login", adminEmail, adminPassword)
	if adminToken == "" {
		return "", errors.New("Kitsu admin authentication failed")
	}
	people, err := listRuntimeBotPeople(host, adminToken)
	if err != nil {
		return "", err
	}
	for _, person := range people {
		if strings.EqualFold(strings.TrimSpace(person.Email), strings.TrimSpace(tempEmail)) {
			return "", errors.New("replacement email already exists; refusing duplicate preparation")
		}
	}
	payload := map[string]interface{}{"first_name": "KitsuSync", "last_name": "Bot", "email": tempEmail, "password": tempPassword, "role": "admin", "active": true, "is_bot": true}
	var created runtimeBotReplacementPerson
	if err := kitsuJSON(adminToken, http.MethodPost, normalizeKitsuHostname(host)+"api/data/persons", payload, &created); err != nil {
		return "", err
	}
	if created.ID == "" || !created.IsBot || !strings.EqualFold(created.Email, tempEmail) {
		if created.ID != "" {
			_ = kitsuJSON(adminToken, http.MethodDelete, normalizeKitsuHostname(host)+"api/data/persons/"+created.ID, nil, nil)
		}
		return "", errors.New("replacement bot ownership verification failed")
	}
	createdSummary := fmt.Sprintf("created replacement id=%s email=%s is_bot=%t active=%t archived=%t role=%s password_hash=not returned by public API", created.ID, created.Email, created.IsBot, created.Active, created.Archived, created.Role)
	if err := verifyReplacementPasswordHash(host, adminToken, created.ID, tempPassword); err != nil {
		if cleanupErr := kitsuJSON(adminToken, http.MethodDelete, normalizeKitsuHostname(host)+"api/data/persons/"+created.ID, nil, nil); cleanupErr != nil {
			return "", fmt.Errorf("%s; password verification failed and rollback failed: %w", createdSummary, cleanupErr)
		}
		return "", fmt.Errorf("%s; password verification failed and rollback succeeded: %w", createdSummary, err)
	}
	createdSummary = fmt.Sprintf("created replacement id=%s email=%s is_bot=%t active=%t archived=%t role=%s password_hash=present bcrypt=verified", created.ID, created.Email, created.IsBot, created.Active, created.Archived, created.Role)
	if err := RecoverRuntimeCredentials(db, host, tempEmail, tempPassword); err != nil {
		if cleanupErr := kitsuJSON(adminToken, http.MethodDelete, normalizeKitsuHostname(host)+"api/data/persons/"+created.ID, nil, nil); cleanupErr != nil {
			return "", fmt.Errorf("replacement recovery failed and rollback failed: %w", cleanupErr)
		}
		return "", fmt.Errorf("%s; replacement recovery failed and rollback succeeded: %w", createdSummary, err)
	}
	return created.ID, nil
}

func FinalizeRuntimeBotReplacement(db *gorm.DB, host, adminEmail, adminPassword, oldID, tempID, tempEmail string) error {
	if db == nil || strings.TrimSpace(oldID) == "" || strings.TrimSpace(tempID) == "" {
		return errors.New("replacement state is incomplete")
	}
	adminToken := basicauth.AuthForJWTToken(normalizeKitsuHostname(host)+"api/auth/login", adminEmail, adminPassword)
	if adminToken == "" {
		return errors.New("Kitsu admin authentication failed")
	}
	people, err := listRuntimeBotPeople(host, adminToken)
	if err != nil {
		return err
	}
	var oldPerson, replacement *runtimeBotReplacementPerson
	canonicalOtherCount := 0
	for i := range people {
		person := &people[i]
		if person.ID == oldID && strings.EqualFold(person.Email, runtimeBotEmail) && person.IsBot && person.Active && !person.Archived {
			oldPerson = person
		}
		if person.ID == tempID && strings.EqualFold(person.Email, tempEmail) && person.IsBot && person.Active && !person.Archived {
			replacement = person
		}
		if strings.EqualFold(person.Email, runtimeBotEmail) && person.ID != oldID && person.ID != tempID {
			canonicalOtherCount++
		}
	}
	if replacement == nil {
		for i := range people {
			person := &people[i]
			if person.ID == tempID && strings.EqualFold(person.Email, runtimeBotEmail) && person.IsBot && person.Active && !person.Archived {
				replacement = person
			}
		}
	}
	if replacement == nil || canonicalOtherCount > 0 {
		return errors.New("replacement ownership verification failed")
	}
	if oldPerson != nil {
		if err := kitsuJSON(adminToken, http.MethodDelete, normalizeKitsuHostname(host)+"api/data/persons/"+oldID, nil, nil); err != nil {
			return fmt.Errorf("old bot deletion failed: %w", err)
		}
	}
	if !strings.EqualFold(replacement.Email, runtimeBotEmail) {
		payload := map[string]interface{}{"first_name": runtimeBotFirstName, "last_name": runtimeBotLastName, "email": runtimeBotEmail, "role": "admin", "active": true, "archived": false, "is_bot": true}
		if err := kitsuJSON(adminToken, http.MethodPut, normalizeKitsuHostname(host)+"api/data/persons/"+tempID, payload, nil); err != nil {
			return fmt.Errorf("replacement rename failed; keep the temporary identity and retry finalize: %w", err)
		}
	}
	password := StoredRuntimeKitsuPassword(db)
	if password == "" {
		return errors.New("replacement password is not persisted")
	}
	return RecoverRuntimeCredentials(db, host, runtimeBotEmail, password)
}
