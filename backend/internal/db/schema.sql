-- ai-support 自己的資料庫 schema——只管業主帳號、登入、業主底下的服務
-- （business）與其內容設定，以及消費端對話的紀錄（conversations/messages）。
-- 實際的 LLM 推理完全由 onagent 負責，這裡不存任何 tool schema。

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

-- ---------------------------------------------------------------------
-- 訂閱方案與用量（由 onagent 的 internal/quota 移植而來，之後會抽成共用套件）
-- 這裡的用量由 ai-support 自己計算，與 onagent 的額度無關。
-- ---------------------------------------------------------------------

-- 每個業主一列，註冊時建立（session.Register）。額度「不」存在這裡，而是
-- 由 tier 對應的方案在查詢時推算（internal/quota.PlanFor），改方案數字時
-- 不需要 migration。monthly_quota 是可選的個人覆寫（NULL = 用方案值）。
-- 計費週期由 started_at 推算，不靠排程重置。
CREATE TABLE IF NOT EXISTS subscriptions (
    user_id       BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    tier          TEXT NOT NULL DEFAULT 'free', -- 自由文字、非 enum，新增方案不需要 migration
    monthly_quota INTEGER,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(), -- 計費週期的錨點
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 只新增不修改的用量帳本；當期用量永遠是對這張表 SUM 出來的，不是計數器。
-- 計費對象是 owner_id 而非 business_id：帳本必須比它所計費的對象活得更久，
-- 否則刪除再重建 business 就能把當月額度歸零。
CREATE TABLE IF NOT EXISTS usage_events (
    id                BIGSERIAL PRIMARY KEY,
    business_id       BIGINT REFERENCES businesses (id) ON DELETE SET NULL, -- business 刪除後保留帳本，僅此欄變 NULL
    owner_id          BIGINT REFERENCES users (id) ON DELETE CASCADE,
    event_id          TEXT NOT NULL,  -- 僅供稽核，不是去重鍵
    kind              TEXT NOT NULL DEFAULT 'prompt',
    prompt_tokens     INT,
    completion_tokens INT,
    total_tokens      INT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 每個強制檢查點都用這兩個欄位查「這個業主自某時間起用了多少」。
CREATE INDEX IF NOT EXISTS usage_events_owner_id_created_at_idx
    ON usage_events (owner_id, created_at);
CREATE INDEX IF NOT EXISTS usage_events_business_id_created_at_idx
    ON usage_events (business_id, created_at);

-- ---------------------------------------------------------------------
-- 金流（TapPay）。與 quota 的 subscriptions（方案/額度）刻意分開：
-- subscriptions 只說「這個業主現在是什麼方案」，這裡記「怎麼收錢」。
-- 卡號完全不經過、也不存在這個後端；只保存 TapPay 回傳的 card_key /
-- card_token（續扣用）與卡片末四碼。價格在程式碼 internal/billing/prices.go，
-- 前端傳來的金額一律不採信。
-- ---------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS billing_profiles (
    user_id          BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    tier             TEXT NOT NULL,                -- 正在付費的方案
    status           TEXT NOT NULL,                -- active | past_due | canceled | expired
    card_key         TEXT NOT NULL DEFAULT '',
    card_token       TEXT NOT NULL DEFAULT '',
    card_last_four   TEXT NOT NULL DEFAULT '',
    anchor_at        TIMESTAMPTZ NOT NULL,         -- 首次付款時間；第 N 期結束 = anchor + N 個月
    periods_paid     INTEGER NOT NULL DEFAULT 0,
    current_period_end TIMESTAMPTZ NOT NULL,
    next_charge_at   TIMESTAMPTZ,                  -- NULL = 不再續扣
    failed_attempts  INTEGER NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS billing_profiles_next_charge_idx
    ON billing_profiles (next_charge_at) WHERE next_charge_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS payments (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind                TEXT NOT NULL,   -- initial | renewal
    tier                TEXT NOT NULL,
    order_number        TEXT NOT NULL,   -- 送給 TapPay 的訂單編號
    amount              INTEGER NOT NULL,
    status              TEXT NOT NULL,   -- pending | succeeded | failed
    rec_trade_id        TEXT NOT NULL DEFAULT '',
    bank_transaction_id TEXT NOT NULL DEFAULT '',
    gateway_status      INTEGER,         -- TapPay 回傳的 status，0 = 成功
    message             TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS payments_order_number_idx ON payments (order_number);

-- 同一個業主同一時間只能有一筆「結果未定」的扣款：既擋重複點擊，也讓
-- 結果不明（逾時）的扣款在人工對帳前不會被再扣一次。
CREATE UNIQUE INDEX IF NOT EXISTS payments_one_pending_per_user_idx
    ON payments (user_id) WHERE status = 'pending';

-- ---------------------------------------------------------------------
-- 消費端對話紀錄。匿名訪客的訊息先送到本後端（記錄 + 額度把關），後端回傳後
-- 瀏覽器才轉送 onagent；AI 的回覆由瀏覽器回報（見 internal/public）。
-- conversations.id 是不可猜的隨機字串，兼作該段對話的匿名憑證。
-- ---------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS conversations (
    id          TEXT PRIMARY KEY,
    business_id BIGINT NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS conversations_business_id_idx ON conversations (business_id, created_at DESC);

CREATE TABLE IF NOT EXISTS messages (
    id              BIGSERIAL PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content         TEXT NOT NULL,
    reply_to        BIGINT REFERENCES messages (id) ON DELETE CASCADE, -- assistant 訊息指向它回覆的 user 訊息
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS messages_conversation_id_idx ON messages (conversation_id, id);

-- 每則 user 訊息最多只接受一則回覆：限制瀏覽器回報（可偽造）所能灌入的量。
CREATE UNIQUE INDEX IF NOT EXISTS messages_one_reply_idx ON messages (reply_to) WHERE reply_to IS NOT NULL;

-- ---------------------------------------------------------------------
-- 服務的外觀設定與編輯器狀態（後台「形象設定」與「AI 可以回答的內容」分類卡片）
-- ---------------------------------------------------------------------
-- 先檢查欄位是否已存在才 ALTER：ALTER TABLE 即使欄位已在也會先取得資料表的排他鎖，
-- 每次啟動都執行會讓正在服務的連線被卡住，多個程序同時啟動時還可能互相死結。
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'businesses' AND column_name = 'tagline') THEN
        ALTER TABLE businesses ADD COLUMN tagline TEXT NOT NULL DEFAULT '';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'businesses' AND column_name = 'mascot') THEN
        ALTER TABLE businesses ADD COLUMN mascot TEXT NOT NULL DEFAULT 'fox';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'businesses' AND column_name = 'theme_color') THEN
        ALTER TABLE businesses ADD COLUMN theme_color TEXT NOT NULL DEFAULT '#FF8A5B';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'business_content' AND column_name = 'sections') THEN
        ALTER TABLE business_content ADD COLUMN sections TEXT;
    END IF;
END $$;
