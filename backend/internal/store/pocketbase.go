// Package store is a thin REST wrapper around PocketBase. You've built
// on PocketBase before (PronounceAI), so this stays deliberately simple
// rather than pulling in a generated SDK — a handful of HTTP calls is
// less to fight with under hackathon time pressure.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/yourusername/tradeguard/internal/models"
)

const (
	collectionTrades    = "proposed_trades"
	collectionAudit     = "audit_log"
	collectionSnapshots = "account_snapshots"
	collectionConfig    = "system_config"
)

type Store struct {
	baseURL string
	http    *http.Client
	token   string
}

func New(baseURL string) *Store {
	return &Store{baseURL: baseURL, http: &http.Client{}}
}

// Authenticate logs in as an admin/superuser so the backend can write
// records regardless of collection API rules.
//
// NOTE: PocketBase renamed the admin auth endpoint in newer versions
// (_superusers vs the older /api/admins path). Check which your
// PocketBase version uses and adjust authPath below if this 404s.
func (s *Store) Authenticate(ctx context.Context, email, password string) error {
	const authPath = "/api/collections/_superusers/auth-with-password"

	body, _ := json.Marshal(map[string]string{
		"identity": email,
		"password": password,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+authPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("pocketbase auth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pocketbase auth failed with status %d (check authPath for your PocketBase version)", resp.StatusCode)
	}

	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return err
	}
	s.token = parsed.Token
	return nil
}

func (s *Store) CreateProposedTrade(ctx context.Context, t models.ProposedTrade) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := s.create(ctx, collectionTrades, t, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (s *Store) CreateAuditEntry(ctx context.Context, e models.AuditLogEntry) error {
	return s.create(ctx, collectionAudit, e, nil)
}

// CreateAccountSnapshot writes a point-in-time account snapshot for the
// Flutter portfolio screen's equity card. Populates the OPTIONAL
// account_snapshots collection — see that collection's schema in the
// README. Safe to call even if the collection doesn't exist yet; the
// caller (agent.RunOnce) logs and continues rather than failing the
// whole pass on it, same treatment as every other optional context
// source in this codebase.
func (s *Store) CreateAccountSnapshot(ctx context.Context, snap models.AccountSnapshot) error {
	return s.create(ctx, collectionSnapshots, snap, nil)
}

// EnsureSystemConfig makes sure exactly one system_config record
// exists, creating it (paused: false) if the collection is empty. Call
// this once at startup — cmd/server does. Idempotent: safe to call on
// every restart, does nothing if a record's already there.
func (s *Store) EnsureSystemConfig(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/collections/%s/records?perPage=1", s.baseURL, collectionConfig)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	s.authorize(req)

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var parsed struct {
		Items []models.SystemConfig `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return err
	}
	if len(parsed.Items) > 0 {
		return nil // already seeded
	}

	return s.create(ctx, collectionConfig, models.SystemConfig{Paused: false}, nil)
}

// IsPaused checks the kill switch. Called by both the agent (skip
// proposing new trades) and the executor (skip executing approved
// ones) — a pause should stop both new decisions and pending actions,
// not just one half of the loop. Fails open to "not paused" on a read
// error rather than silently halting the whole system because of a
// transient network hiccup — see the doc comment at each call site for
// why that's the right default here.
func (s *Store) IsPaused(ctx context.Context) (bool, error) {
	url := fmt.Sprintf("%s/api/collections/%s/records?perPage=1", s.baseURL, collectionConfig)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	s.authorize(req)

	resp, err := s.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var parsed struct {
		Items []models.SystemConfig `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return false, err
	}
	if len(parsed.Items) == 0 {
		return false, nil // not seeded yet — treat as not paused
	}
	return parsed.Items[0].Paused, nil
}

// ApprovedTrades returns proposed trades whose status is "approved" —
// polled by internal/execution.Executor. Swap this for a PocketBase
// realtime subscription later if you want push-style updates instead
// of polling; polling is simpler to get right in a hackathon timeframe.
func (s *Store) ApprovedTrades(ctx context.Context) ([]models.ProposedTrade, error) {
	url := fmt.Sprintf("%s/api/collections/%s/records?filter=(status='approved')",
		s.baseURL, collectionTrades)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	s.authorize(req)

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed struct {
		Items []models.ProposedTrade `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Items, nil
}

func (s *Store) UpdateTradeStatus(ctx context.Context, id string, status models.Status) error {
	url := fmt.Sprintf("%s/api/collections/%s/records/%s", s.baseURL, collectionTrades, id)

	body, _ := json.Marshal(map[string]string{"status": string(status)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	s.authorize(req)

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update trade status failed with status %d: %s", resp.StatusCode, errBody)
	}
	return nil
}

func (s *Store) create(ctx context.Context, collection string, payload any, out any) error {
	url := fmt.Sprintf("%s/api/collections/%s/records", s.baseURL, collection)

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	s.authorize(req)

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create record in %s failed with status %d: %s", collection, resp.StatusCode, errBody)
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (s *Store) authorize(req *http.Request) {
	if s.token != "" {
		req.Header.Set("Authorization", s.token)
	}
}
