package store

import (
	"context"
	"github.com/KennyMx/Relay/internal/keys"
	"github.com/KennyMx/Relay/internal/provider"
	"github.com/KennyMx/Relay/internal/router"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Store struct{ Pool *pgxpool.Pool }
type Key struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	RequestsPerMinute int       `json:"requests_per_minute"`
	Burst             int       `json:"burst"`
	CreatedAt         time.Time `json:"created_at"`
}

func (s *Store) CreateKey(ctx context.Context, name string, rpm, burst int) (Key, string, error) {
	raw, hash, err := keys.New()
	if err != nil {
		return Key{}, "", err
	}
	k := Key{ID: keys.ID(), Name: name, RequestsPerMinute: rpm, Burst: burst}
	err = s.Pool.QueryRow(ctx, "INSERT INTO relay_keys(id,key_hash,name,requests_per_minute,burst) VALUES($1,$2,$3,$4,$5) RETURNING created_at", k.ID, hash, name, rpm, burst).Scan(&k.CreatedAt)
	if err != nil {
		return Key{}, "", err
	}
	return k, raw, nil
}
func (s *Store) ValidateKey(ctx context.Context, raw string) (Key, error) {
	hash, err := keys.Hash(raw)
	if err != nil {
		return Key{}, err
	}
	var k Key
	err = s.Pool.QueryRow(ctx, "SELECT id,name,requests_per_minute,burst,created_at FROM relay_keys WHERE key_hash=$1 AND revoked_at IS NULL", hash).Scan(&k.ID, &k.Name, &k.RequestsPerMinute, &k.Burst, &k.CreatedAt)
	return k, err
}
func (s *Store) RevokeKey(ctx context.Context, id string) (bool, error) {
	result, err := s.Pool.Exec(ctx, "UPDATE relay_keys SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL", id)
	return result.RowsAffected() > 0, err
}

type Record struct {
	ID            string           `json:"id"`
	KeyID         string           `json:"-"`
	CreatedAt     time.Time        `json:"created_at"`
	Route         string           `json:"route"`
	Model         string           `json:"model"`
	Provider      string           `json:"provider"`
	Usage         provider.Usage   `json:"usage"`
	LatencyMS     int64            `json:"latency_ms"`
	CostNanoUSD   int64            `json:"cost_nano_usd"`
	Status        string           `json:"status"`
	ErrorCode     string           `json:"error_code,omitempty"`
	FallbackCount int              `json:"fallback_count"`
	Attempts      []router.Attempt `json:"attempts,omitempty"`
}

// Begin ensures an audit record exists before any upstream work starts.
func (s *Store) Begin(ctx context.Context, r Record) error {
	_, err := s.Pool.Exec(ctx, "INSERT INTO requests(id,key_id,created_at,route,status) VALUES($1,$2,$3,$4,'pending')", r.ID, r.KeyID, r.CreatedAt, r.Route)
	return err
}

// Finish commits the final request and every attempt atomically.
func (s *Store) Finish(ctx context.Context, r Record) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE requests SET model=$2,provider=$3,input_tokens=$4,output_tokens=$5,total_tokens=$6,simulated=$7,latency_ms=$8,cost_nano_usd=$9,status=$10,error_code=$11,fallback_count=$12 WHERE id=$1`, r.ID, r.Model, r.Provider, r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.TotalTokens, r.Usage.Simulated, r.LatencyMS, r.CostNanoUSD, r.Status, r.ErrorCode, r.FallbackCount)
	if err != nil {
		return err
	}
	for _, a := range r.Attempts {
		_, err = tx.Exec(ctx, `INSERT INTO provider_attempts(request_id,number,provider,model,started_at,latency_ms,status,error_code,input_tokens,output_tokens,total_tokens,simulated,cost_nano_usd) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, r.ID, a.Number, a.Provider, a.Model, a.StartedAt, a.LatencyMS, a.Status, a.ErrorCode, a.Usage.InputTokens, a.Usage.OutputTokens, a.Usage.TotalTokens, a.Usage.Simulated, a.CostNanoUSD)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const columns = `id,key_id,created_at,route,model,provider,input_tokens,output_tokens,total_tokens,simulated,latency_ms,cost_nano_usd,status,error_code,fallback_count`

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner) (Record, error) {
	var r Record
	err := row.Scan(&r.ID, &r.KeyID, &r.CreatedAt, &r.Route, &r.Model, &r.Provider, &r.Usage.InputTokens, &r.Usage.OutputTokens, &r.Usage.TotalTokens, &r.Usage.Simulated, &r.LatencyMS, &r.CostNanoUSD, &r.Status, &r.ErrorCode, &r.FallbackCount)
	return r, err
}
func (s *Store) List(ctx context.Context, keyID string, limit, offset int) ([]Record, error) {
	rows, err := s.Pool.Query(ctx, "SELECT "+columns+" FROM requests WHERE key_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3", keyID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]Record, 0)
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}
func (s *Store) Get(ctx context.Context, keyID, id string) (Record, error) {
	r, err := scanRecord(s.Pool.QueryRow(ctx, "SELECT "+columns+" FROM requests WHERE key_id=$1 AND id=$2", keyID, id))
	if err != nil {
		return r, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT number,provider,model,started_at,latency_ms,status,error_code,input_tokens,output_tokens,total_tokens,simulated,cost_nano_usd FROM provider_attempts WHERE request_id=$1 ORDER BY number`, id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	r.Attempts = make([]router.Attempt, 0)
	for rows.Next() {
		var a router.Attempt
		err = rows.Scan(&a.Number, &a.Provider, &a.Model, &a.StartedAt, &a.LatencyMS, &a.Status, &a.ErrorCode, &a.Usage.InputTokens, &a.Usage.OutputTokens, &a.Usage.TotalTokens, &a.Usage.Simulated, &a.CostNanoUSD)
		if err != nil {
			return r, err
		}
		r.Attempts = append(r.Attempts, a)
	}
	return r, rows.Err()
}
