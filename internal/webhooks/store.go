// Package webhooks delivers signed event notifications to customer-registered
// HTTP endpoints. Delivery runs as River jobs so retries, backoff, and
// concurrency come for free; every attempt is recorded for the dashboard.
package webhooks

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Endpoint struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	URL       string
	Secret    string
	Enabled   bool
	CreatedAt time.Time
}

type DeliveryStatus string

const (
	StatusPending   DeliveryStatus = "pending"
	StatusDelivered DeliveryStatus = "delivered"
	StatusFailed    DeliveryStatus = "failed"
)

type Delivery struct {
	ID             uuid.UUID
	EndpointID     uuid.UUID
	AccountID      uuid.UUID
	Event          string
	Payload        []byte
	Status         DeliveryStatus
	AttemptCount   int
	LastStatusCode *int
	LastError      *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeliveredAt    *time.Time
}

var ErrNotFound = errors.New("webhooks: not found")

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// --- endpoints ---

// CreateEndpoint registers a new endpoint with a freshly generated signing
// secret. The raw secret is returned in the struct so the UI can show it
// once; it's also stored (we need it to sign — this is a shared secret, not
// a password).
func (s *Store) CreateEndpoint(ctx context.Context, accountID uuid.UUID, url string) (Endpoint, error) {
	e := Endpoint{AccountID: accountID, URL: url, Secret: newSecret(), Enabled: true}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO webhook_endpoints (account_id, url, secret)
		VALUES ($1, $2, $3)
		RETURNING id, enabled, created_at
	`, accountID, url, e.Secret).Scan(&e.ID, &e.Enabled, &e.CreatedAt)
	return e, err
}

func (s *Store) ListEndpoints(ctx context.Context, accountID uuid.UUID) ([]Endpoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, url, secret, enabled, created_at
		  FROM webhook_endpoints
		 WHERE account_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Endpoint
	for rows.Next() {
		var e Endpoint
		if err := rows.Scan(&e.ID, &e.AccountID, &e.URL, &e.Secret, &e.Enabled, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListEnabledEndpoints returns endpoints eligible for delivery.
func (s *Store) ListEnabledEndpoints(ctx context.Context, accountID uuid.UUID) ([]Endpoint, error) {
	all, err := s.ListEndpoints(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, e := range all {
		if e.Enabled {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *Store) GetEndpoint(ctx context.Context, accountID, endpointID uuid.UUID) (Endpoint, error) {
	var e Endpoint
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, url, secret, enabled, created_at
		  FROM webhook_endpoints
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, endpointID, accountID).Scan(&e.ID, &e.AccountID, &e.URL, &e.Secret, &e.Enabled, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Endpoint{}, ErrNotFound
	}
	return e, err
}

func (s *Store) GetEndpointByID(ctx context.Context, endpointID uuid.UUID) (Endpoint, error) {
	var e Endpoint
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, url, secret, enabled, created_at
		  FROM webhook_endpoints WHERE id = $1 AND deleted_at IS NULL
	`, endpointID).Scan(&e.ID, &e.AccountID, &e.URL, &e.Secret, &e.Enabled, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Endpoint{}, ErrNotFound
	}
	return e, err
}

// DeleteEndpoint soft-deletes; existing deliveries are retained for the log.
func (s *Store) DeleteEndpoint(ctx context.Context, accountID, endpointID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE webhook_endpoints SET deleted_at = now()
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, endpointID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- deliveries ---

// CreateDelivery records a pending delivery row. The caller enqueues the
// River job separately, keyed on the returned ID.
func (s *Store) CreateDelivery(ctx context.Context, endpointID, accountID uuid.UUID, event string, payload []byte) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO webhook_deliveries (endpoint_id, account_id, event, payload)
		VALUES ($1, $2, $3, $4::jsonb)
		RETURNING id
	`, endpointID, accountID, event, string(payload)).Scan(&id)
	return id, err
}

func (s *Store) GetDelivery(ctx context.Context, id uuid.UUID) (Delivery, error) {
	var d Delivery
	err := s.pool.QueryRow(ctx, `
		SELECT id, endpoint_id, account_id, event, payload, status, attempt_count,
		       last_status_code, last_error, created_at, updated_at, delivered_at
		  FROM webhook_deliveries WHERE id = $1
	`, id).Scan(&d.ID, &d.EndpointID, &d.AccountID, &d.Event, &d.Payload, &d.Status,
		&d.AttemptCount, &d.LastStatusCode, &d.LastError, &d.CreatedAt, &d.UpdatedAt, &d.DeliveredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	return d, err
}

func (s *Store) ListDeliveries(ctx context.Context, accountID, endpointID uuid.UUID, limit int) ([]Delivery, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, endpoint_id, account_id, event, payload, status, attempt_count,
		       last_status_code, last_error, created_at, updated_at, delivered_at
		  FROM webhook_deliveries
		 WHERE account_id = $1 AND endpoint_id = $2
		 ORDER BY created_at DESC
		 LIMIT $3
	`, accountID, endpointID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.EndpointID, &d.AccountID, &d.Event, &d.Payload, &d.Status,
			&d.AttemptCount, &d.LastStatusCode, &d.LastError, &d.CreatedAt, &d.UpdatedAt, &d.DeliveredAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RecordAttempt updates a delivery row after a delivery attempt. statusCode is
// 0 when the request never got a response (DNS/connection error).
func (s *Store) RecordAttempt(ctx context.Context, id uuid.UUID, status DeliveryStatus, statusCode int, attemptErr string) error {
	var codeArg any
	if statusCode != 0 {
		codeArg = statusCode
	}
	var errArg any
	if attemptErr != "" {
		errArg = attemptErr
	}
	var deliveredAt any
	if status == StatusDelivered {
		deliveredAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		   SET status = $2,
		       attempt_count = attempt_count + 1,
		       last_status_code = $3,
		       last_error = $4,
		       updated_at = now(),
		       delivered_at = COALESCE($5, delivered_at)
		 WHERE id = $1
	`, id, status, codeArg, errArg, deliveredAt)
	return err
}

func newSecret() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic("webhooks: cannot read random bytes: " + err.Error())
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(b)
}

// EventPayload is the JSON body shape we POST to endpoints.
type EventPayload struct {
	Event     string         `json:"event"`
	AccountID string         `json:"account_id"`
	CreatedAt time.Time      `json:"created_at"`
	Data      map[string]any `json:"data"`
}

// MarshalPayload builds the canonical JSON body for an event.
func MarshalPayload(event, accountID string, data map[string]any) ([]byte, error) {
	return json.Marshal(EventPayload{
		Event:     event,
		AccountID: accountID,
		CreatedAt: time.Now().UTC(),
		Data:      data,
	})
}
