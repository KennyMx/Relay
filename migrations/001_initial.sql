CREATE TABLE relay_keys (
 id text PRIMARY KEY,
 key_hash text UNIQUE NOT NULL,
 name text NOT NULL,
 requests_per_minute integer NOT NULL CHECK (requests_per_minute BETWEEN 1 AND 100000),
 burst integer NOT NULL CHECK (burst BETWEEN 1 AND 100000),
 created_at timestamptz NOT NULL DEFAULT now(),
 revoked_at timestamptz
);
CREATE TABLE requests (
 id text PRIMARY KEY,
 key_id text NOT NULL REFERENCES relay_keys(id),
 created_at timestamptz NOT NULL,
 route text NOT NULL,
 model text NOT NULL DEFAULT '',
 provider text NOT NULL DEFAULT '',
 input_tokens bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
 output_tokens bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
 total_tokens bigint NOT NULL DEFAULT 0 CHECK (total_tokens = input_tokens + output_tokens),
 simulated boolean NOT NULL DEFAULT false,
 latency_ms bigint NOT NULL DEFAULT 0,
 cost_nano_usd bigint NOT NULL DEFAULT 0 CHECK (cost_nano_usd >= 0),
 status text NOT NULL CHECK (status IN ('pending','success','error')),
 error_code text NOT NULL DEFAULT '',
 fallback_count integer NOT NULL DEFAULT 0
);
CREATE INDEX requests_key_created ON requests(key_id, created_at DESC, id DESC);
CREATE TABLE provider_attempts (
 request_id text NOT NULL REFERENCES requests(id),
 number integer NOT NULL,
 provider text NOT NULL,
 model text NOT NULL,
 started_at timestamptz NOT NULL,
 latency_ms bigint NOT NULL,
 status text NOT NULL CHECK (status IN ('success','error')),
 error_code text NOT NULL,
 input_tokens bigint NOT NULL CHECK (input_tokens >= 0),
 output_tokens bigint NOT NULL CHECK (output_tokens >= 0),
 total_tokens bigint NOT NULL CHECK (total_tokens = input_tokens + output_tokens),
 simulated boolean NOT NULL,
 cost_nano_usd bigint NOT NULL CHECK (cost_nano_usd >= 0),
 PRIMARY KEY (request_id,number)
);
