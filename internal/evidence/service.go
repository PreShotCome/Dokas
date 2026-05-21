package evidence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultRetention is how long evidence must be kept — auditor requirement.
const DefaultRetention = 7 * 365 * 24 * time.Hour

// Service ties the evidence store, signer, and signature records together.
// The drill report worker calls Finalize; handlers call Verify.
type Service struct {
	store     Store
	signer    *Signer
	pool      *pgxpool.Pool
	retention time.Duration
}

func NewService(store Store, signer *Signer, pool *pgxpool.Pool) *Service {
	return &Service{store: store, signer: signer, pool: pool, retention: DefaultRetention}
}

// Finalize stores the rendered PDF, signs it, and records the signature with
// a retain_until horizon. Returns the storage key for drills.evidence_path.
func (s *Service) Finalize(ctx context.Context, drillID uuid.UUID, pdf []byte) (string, error) {
	key, err := s.store.Put(ctx, drillID.String(), pdf)
	if err != nil {
		return "", err
	}

	// Truncate to microseconds: Postgres TIMESTAMPTZ has microsecond
	// precision, so the value the signature covers must already be at that
	// precision or it won't survive the DB round-trip for verification.
	signedAt := time.Now().UTC().Truncate(time.Microsecond)
	sig := s.signer.Sign(pdf, signedAt)
	retainUntil := signedAt.Add(s.retention)

	_, err = s.pool.Exec(ctx, `
		INSERT INTO evidence_signatures
		    (drill_id, algorithm, public_key_id, signature, pdf_sha256, signed_at, retain_until)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (drill_id) DO UPDATE SET
		    algorithm = EXCLUDED.algorithm,
		    public_key_id = EXCLUDED.public_key_id,
		    signature = EXCLUDED.signature,
		    pdf_sha256 = EXCLUDED.pdf_sha256,
		    signed_at = EXCLUDED.signed_at,
		    retain_until = EXCLUDED.retain_until
	`, drillID, sig.Algorithm, sig.PublicKeyID, sig.Value, sig.PDFSHA256, signedAt, retainUntil)
	if err != nil {
		return "", fmt.Errorf("record signature: %w", err)
	}
	return key, nil
}

// SignatureRecord is a stored signature plus its retention horizon.
type SignatureRecord struct {
	Signature
	RetainUntil time.Time
}

var ErrNoSignature = errors.New("evidence: no signature for drill")

// GetSignature loads the signature row for a drill.
func (s *Service) GetSignature(ctx context.Context, drillID uuid.UUID) (SignatureRecord, error) {
	var rec SignatureRecord
	err := s.pool.QueryRow(ctx, `
		SELECT algorithm, public_key_id, signature, pdf_sha256, signed_at, retain_until
		  FROM evidence_signatures WHERE drill_id = $1
	`, drillID).Scan(&rec.Algorithm, &rec.PublicKeyID, &rec.Value, &rec.PDFSHA256,
		&rec.SignedAt, &rec.RetainUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return SignatureRecord{}, ErrNoSignature
	}
	return rec, err
}

// VerifyResult is the outcome of re-verifying a drill's evidence.
type VerifyResult struct {
	Signed      bool
	Valid       bool
	Reason      string // populated when Valid is false
	PublicKeyID string
	SignedAt    time.Time
	RetainUntil time.Time
}

// Verify re-reads the stored PDF and checks it against the recorded
// signature. A mismatch (tampered file, wrong key) yields Valid=false with a
// human-readable Reason rather than an error.
func (s *Service) Verify(ctx context.Context, drillID uuid.UUID, key string) (VerifyResult, error) {
	rec, err := s.GetSignature(ctx, drillID)
	if errors.Is(err, ErrNoSignature) {
		return VerifyResult{Signed: false}, nil
	}
	if err != nil {
		return VerifyResult{}, err
	}

	res := VerifyResult{
		Signed:      true,
		PublicKeyID: rec.PublicKeyID,
		SignedAt:    rec.SignedAt,
		RetainUntil: rec.RetainUntil,
	}

	if rec.PublicKeyID != s.signer.PublicKeyID() {
		res.Reason = "signed with a key this server does not hold (key rotated?)"
		return res, nil
	}
	pdf, err := s.store.ReadAll(ctx, key)
	if err != nil {
		res.Reason = "evidence file unreadable: " + err.Error()
		return res, nil
	}
	if err := Verify(s.signer.PublicKey(), pdf, rec.Signature); err != nil {
		res.Reason = err.Error()
		return res, nil
	}
	res.Valid = true
	return res, nil
}

// Open returns a reader for a stored evidence key.
func (s *Service) Open(ctx context.Context, key string) (readCloser, error) {
	return s.store.Open(ctx, key)
}

// DeleteKey removes an evidence object. Used by the account hard-delete
// (crypto-shred) path, which is exempt from retention because the whole
// account is being erased on the customer's instruction.
func (s *Service) DeleteKey(ctx context.Context, key string) error {
	return s.store.Delete(ctx, key)
}

// PurgeExpired deletes evidence whose retain_until has passed: the stored
// file and the signature row. Returns the number of drills purged. This is
// the Object-Lock analogue — nothing is removed before retain_until.
func (s *Service) PurgeExpired(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.drill_id, d.evidence_path
		  FROM evidence_signatures g
		  JOIN drills d ON d.id = g.drill_id
		 WHERE g.retain_until < now()
	`)
	if err != nil {
		return 0, err
	}
	type expired struct {
		id   uuid.UUID
		path *string
	}
	var list []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.id, &e.path); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, e)
	}
	rows.Close()

	purged := 0
	for _, e := range list {
		if e.path != nil && *e.path != "" {
			if err := s.store.Delete(ctx, *e.path); err != nil {
				return purged, fmt.Errorf("delete evidence %s: %w", e.id, err)
			}
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM evidence_signatures WHERE drill_id = $1`, e.id); err != nil {
			return purged, err
		}
		purged++
	}
	return purged, nil
}

// readCloser is a tiny alias so callers don't need to import io.
type readCloser = interface {
	Read([]byte) (int, error)
	Close() error
}
