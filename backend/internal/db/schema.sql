-- ai-support 自己的資料庫 schema——只管業主帳號、登入、業主底下的服務
-- （business）與其內容設定。實際的 LLM 推理/tool-calling 完全由 onagent
-- 負責，這裡不存任何 tool schema 或對話紀錄。

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL,
    password_hash TEXT, -- bcrypt; NULL for an account that has never set a password (Google-only)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_idx ON users (lower(email));

CREATE TABLE IF NOT EXISTS identities (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider          TEXT NOT NULL,   -- 'google' today
    provider_user_id  TEXT NOT NULL,
    provider_email    TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS identities_user_id_idx ON identities (user_id);

CREATE UNIQUE INDEX IF NOT EXISTS identities_provider_provider_user_id_idx
    ON identities (provider, provider_user_id);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY, -- opaque random token; also the cookie value
    user_id    BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions (user_id);

-- 一個業主帳號（user）底下可以有多個 business（服務）——多租戶的核心：
-- 每個 business 對應 onagent 那邊的一個 app，靠 onagent_app_id/
-- onagent_api_key 呼叫 onagent 的 console API 推送 tool 定義,
-- 消費者頁面則直接用 onagent_api_key 讓瀏覽器端的 @onagent/bridge
-- SDK 開 WebSocket，不經過這個後端轉發。
CREATE TABLE IF NOT EXISTS businesses (
    id               BIGSERIAL PRIMARY KEY,
    owner_id         BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    slug             TEXT NOT NULL, -- 消費者服務頁面網址用，例如 /support/<slug>
    name             TEXT NOT NULL,
    onagent_app_id   TEXT,          -- 對應 onagent 那邊建立的 app id，尚未建立前為 NULL
    onagent_api_key  TEXT,          -- onagent 該 app 的 API key（消費者頁面 SDK 連線用）
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS businesses_owner_id_idx ON businesses (owner_id);

CREATE UNIQUE INDEX IF NOT EXISTS businesses_slug_idx ON businesses (slug);

-- 業主在自己的 console 設定「AI 可以提供的內容」，存起來後由後端呼叫
-- onagent 的 console API 轉成該 business 對應 onagent app 的 tool 定義。
-- 內容結構先保持最單純（單一文字欄位），實際的結構化（例如拆成多筆
-- FAQ）留給之後的產品需求決定，不在空骨架範圍內先假設。
CREATE TABLE IF NOT EXISTS business_content (
    business_id BIGINT PRIMARY KEY REFERENCES businesses (id) ON DELETE CASCADE,
    content     TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
