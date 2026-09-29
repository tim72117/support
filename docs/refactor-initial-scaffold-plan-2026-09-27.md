# ai-support 初始骨架規劃（2026-09-27）

現況：**後端空骨架已建立，可編譯、未串接前端與 onagent；`apps/console`、`apps/support` 前端已完成第一版 UI（假資料，未串接真實後端），尚未實測執行**。這份文件記錄需求討論的完整脈絡與目前進度，供之後開新對話接手實作時使用——不需要重新問一輪已經定案的問題。

## 產品定位

面向消費者的 AI 客服服務。兩方使用者：

- **業主方**（企業）：登入自己的 console，設定「AI 可以提供的資訊」（目前先用單一文字內容欄位，未來若要拆成結構化 FAQ 之類，是後續產品需求，不在這次骨架範圍內先假設）
- **民眾方**（消費者）：每個業主對應一個公開的服務頁面（依 slug，例如 `/support/<slug>`），進去後有對話功能，agent 依業主提供的內容回答

## 核心架構決策（已定案）

1. **推論完全交給 onagent，ai-support 不重做推理引擎**
   - ai-support **不**複製 onagent 的 `internal/inference`/`internal/ws`/`internal/protocol`/`internal/toolschema`/`internal/codegen`
   - **消費者對話頁面**：純前端，直接嵌入 `@onagent/bridge` SDK 連線 onagent，不經過 ai-support 後端轉發
   - **ai-support 後端**：存業主帳號 + 內容設定，透過呼叫 onagent 的 console API（比照 onagent CLI 的做法）把內容推成該業主對應 onagent app 的 tool 定義——**這一段尚未實作**，見下方「未完成」

2. **保留多租戶架構**：一個業主帳號底下可以有多個 business（服務），未來也可能讓多個客戶各自使用。每個 business 對應 onagent 那邊的一個 app（一組 `onagent_app_id` + `onagent_api_key`）

3. **登入機制只用一般使用者登入**：`internal/session`（email/password + cookie）+ `internal/googleauth`（Google OAuth，可選）。**不**複製 onagent 的 CLI 認證（`internal/cliauth`）、admin 後台認證（`internal/adminauth`）、app API key 驗證（`internal/auth`，那個其實是驗證「網站證明自己是哪個 app」用的，跟業主登入是完全不同的兩件事，過程中一度搞混，已釐清）

4. **保留 `apps/console`**：業主自己管理 tool/內容設定的網頁介面（而不是只給你自己用 CLI/YAML 改）

## 已完成的骨架

### 後端（`backend/`，Go module `github.com/tim72117/ai-support`）

已建立、已通過 `go build ./...` 與 `go vet ./...`（offline，用 onagent 現成的 module cache + 複製調整過的 go.sum）：

- `internal/db`：`db.Open` 沿用 onagent 同樣的模式（embed schema.sql，GORM + lib/pq）。`schema.sql` 定義 `users`/`identities`/`sessions`（跟 onagent 幾乎一致的登入用表）+ 新的 `businesses`/`business_content`（多租戶核心）
- `internal/session`：複製 onagent `internal/session`，**拿掉 subscription/quota tier 概念**（那是 onagent 自己的計費模型，跟 ai-support 無關）。註冊/登入/Google 登入/session cookie 驗證/登出，含 `SameSite=None` 需要 `Secure=true` 才生效的既有坑（已保留在複製的程式碼裡，避免重踩）
- `internal/googleauth`：複製 onagent 版本，只換了 import path 跟 state cookie 名稱，介面完全對得上 `session.Store`
- `internal/business`：新套件，管理 `businesses`/`business_content` 兩張表，`Create`/`Get`/`ListByOwner`/`Delete`/`GetContent`/`SetContent`/`SetOnagentApp`
- `internal/console`：業主 console 的 HTTP API——`/auth/register|login|logout|me`、`/console/businesses`（CRUD）、`/console/businesses/{id}/content`（get/put）。Ownership 檢查回 404 不回 403（避免洩漏「這個 id 存在但不是你的」），沿用 onagent `console.go` 的既有原則
- `internal/onagentclient`：**故意留白的骨架**——`CreateApp`/`PushContent` 兩個方法目前都回傳 `ErrNotImplemented`。沒有動手實作是因為 onagent 實際的 console API request/response 格式，這次沒有重新查證過，怕生出一個似是而非的錯誤介面比留白更難察覺、更難修。
- `cmd/server/main.go`：跑起來只服務 `/console/*`、`/auth/*`（CORS 保護），不含任何 WebSocket/inference 相關端點

### 前端（`apps/`）

- `apps/console`：**業主管理介面，第一版 UI 已完成**（React + Vite + TypeScript + CSS Modules，仿 onagent console 的 `tsconfig`/`vite.config` 慣例，`base` 拿掉——ai-support 沒有共用 origin 的 landing 需要 namespace）。**目前完全用假資料跑，沒有串接 `/auth`、`/console` 後端 API**（刻意選擇，見下方「已定案的實作順序」）：
  - `MockBackendContext.tsx`：假的登入 session + businesses 狀態，全部存在 memory，重新整理頁面就登出/重置。之後串真實後端時，這個 context 的介面（`login`/`logout`/`businesses`/`updateBusiness`/…）就是要替換成 fetch 呼叫的地方
  - `Login.tsx`：任何 email 都能「登入」，非技術業主導向的文案（例如「登入管理你的 AI 客服小幫手」而非技術用語）
  - `BusinessList.tsx` + `NewBusinessModal.tsx`：業主的服務列表，卡片顯示吉祥物、對外連結、是否已上線；新增服務時可選吉祥物與主題色
  - `BusinessEditor.tsx`：核心頁面，兩個分頁——「AI 可以回答的內容」用**多個分類卡片**（營業時間/聯絡方式/產品服務/退換貨規則/常見問題/其他）呈現給業主填寫，存檔時用 `mockData.ts` 的 `buildContentText()` 合併成一段文字（對應後端 `business_content` 單一欄位，之後拆結構化 FAQ 不受影響）；「形象設定」分頁可改名稱、簡介、吉祥物、主題色。頁面內有「AI 實際會看到的內容預覽」區塊，讓業主看到文字合併後的樣子
  - `Mascot.tsx`：手繪風格 inline SVG 吉祥物（狐狸/熊/貓/鳥四選一），依業主主題色上色，同一角色設定在 console 預覽與 support 對話頁共用（各自獨立一份程式碼，非共用套件）
  - 已知坑第 2、4 條在這次實作中特別處理：`BusinessEditor.tsx` 的 hooks 全部在條件式 `return` 之前；`NewBusinessModal.tsx` 的「可以建立」判斷跟實際送出用同一個 trim 過的值，不是兩套邏輯
- `apps/support`：**消費者對話頁面，第一版 UI 已完成**，從零建立（`package.json`/`vite.config.ts`/`tsconfig.*`/`index.html`/`src/` 全部新寫）。依網址路徑 `/support/<slug>` 找對應業主（目前 `mockData.ts` 內建兩筆假資料：`chenguang-bakery`、`furry-studio`，找不到 slug 顯示「找不到這個服務頁面」）：
  - `App.tsx`：版面分兩段——上方是業主形象區（主題色底、吉祥物、業主名稱與簡介），下方是對話區。**進頁面先看到「有問題想問我們嗎？」歡迎畫面**，要點擊「開始對話」或點建議問題按鈕才會進入真正的對話介面（對應已知坑第 5 條：不要一載入就搶先建立連線；這裡雖然還沒有真的 WebSocket，但先把「使用者主動開始」這個時機點的 UX 架好，之後接 `@onagent/bridge` 時連線時機直接對應到 `start()` 這個現成的呼叫點）
  - `useDemoChat.ts`：假對話引擎，使用者送出訊息後模擬打字中動畫，回覆固定的示範文字說明「這是示範頁面」。之後串接真實 onagent 時，這個 hook 的介面（`started`/`messages`/`isTyping`/`start`/`send`）就是要替換成 `@onagent/bridge` 呼叫的地方
  - `Mascot.tsx`：跟 console 同一套角色設計但獨立檔案，多了 `animated` 模式（idle 彈跳 + 眨眼動畫，`prefers-reduced-motion` 時關閉）用在歡迎畫面與對話視窗的頭像
- `apps/landing`：**行銷首頁，對象是要說服企業主（業主方）使用這個服務的訪客，不是消費者/民眾方**——跟 onagent 自己的 landing 定位一致（onagent landing 說服的是開發者，這裡說服的是想要 AI 客服的企業）。已建立 `package.json`（純靜態 HTML + Vite，比照 onagent landing 的「Vite 只當 dev server/bundler，不是框架」模式）、`vite.config.ts`、`index.html`（純佔位內容，`<h1>ai-support</h1>` 加一行說明，實際文案/視覺完全沒做）。**這是另一次並行作業加入的骨架，跟這次 console/support 的 UI 工作無關，尚未檢視其內容是否完整**

## 視覺風格要求

**整體風格：清新明亮、乾淨，可以帶一點親民卡通感。** 這個要求同時適用於 `apps/console`（業主介面）跟 `apps/support`（消費者對話頁面），實作前端元件時要反映在配色、圓角、插圖風格等視覺選擇上。

## 前端技術慣例要求

**CSS 元件從一開始就要走完整的 CSS Modules 模式**——每個元件 `Foo.tsx` 搭配自己的 `Foo.module.css`，一對一，不要用全域樣式或內聯樣式頂替（比照 onagent `apps/console` 目前的實際做法）。

## 明確要求「不要踩的坑」（來自 onagent 這個 session 稽核過的真實 bug）

實作前端元件時要主動避開這些已知問題模式：

1. **CSS inline 元素（`<span>` 等）的 `margin` 對上下方向完全無效**——CSS 規範本來就忽略 inline 元素的上下 margin，不管數值多大都沒用。需要間距的元素要用 `display: block`（或改用真正的 block 元素）。
2. **React Hooks 不能放在條件式 `return` 之後**——會觸發「Rendered more hooks than during the previous render」。如果某個值需要依賴「使用者是否登入」「資料是否載入完成」這類早退條件才能算，且運算本身不貴，改用一般表達式而非 `useMemo`/`useCallback`。
3. **巢狀 sheet/modal 的 Escape 鍵處理，要注意事件冒泡範圍**——onagent 曾發生 Escape 同時關掉巢狀子 sheet 跟父層 sheet，一次動作靜默丟棄所有正在編輯的內容。子層要 `stopPropagation` 或明確判斷「目前最上層開著的是哪一個」。
4. **表單「可以儲存」的判斷邏輯，要跟「實際送出」的資料處理方式完全一致**——onagent 曾發生用 trim 過的字串判斷「可以存」，但實際送出時沒有 trim，導致帶尾隨空白的內容通過檢查、悄悄存進去。
5. **不要在頁面一載入就開真實的 WebSocket 連線**——消費者服務頁面嵌入 `@onagent/bridge` 時，如果打算要有「先看到歡迎畫面、點擊才開始對話」這類 UX，連線時機要對應到使用者實際的互動意圖，不要頁面一開就搶先連線（onagent 手機版曾犯過這個錯，使用者根本還沒點開功能就先建立了 WebSocket）。
6. **捨棄變更的確認對話框，取消後畫面不該繼續切換**——onagent 曾發生使用者按下「取消捨棄」，畫面卻還是換到新畫面，未儲存的編輯被靜默覆蓋。任何「離開前確認」的邏輯，要確保取消真的能擋住後續的畫面/狀態切換，不能只是彈出對話框但邏輯繼續往下跑。

## 尚未完成、接手時的優先順序建議

1. **`internal/onagentclient` 的實際實作**——這是整個串接的關鍵缺口。要先去 onagent 的 `backend/internal/console`（CLI 實際呼叫的那組 API）跟 `backend/internal/toolschema`（tool 定義格式）重新查證一次現況，不要憑這份文件裡的舊印象直接動手，因為這份規劃完全沒有驗證過 onagent console API 的實際 request/response 格式。
2. **把 `apps/console` 從假資料換成真實後端**——`MockBackendContext.tsx` 的介面（`login`/`logout`/`businesses`/`getBusiness`/`updateBusiness`/`createBusiness`/`deleteBusiness`）已經對齊 `internal/console` 現有端點的資料形狀，換成 fetch 呼叫時應該是機械式的替換，不需要重新設計元件。要注意 `business_content` 目前後端是單一文字欄位，`BusinessEditor.tsx` 的分類卡片是純前端呈現，存檔送出的是 `buildContentText()` 合併後的字串——串接時後端不需要跟著改結構。
3. **`apps/support` 串接真正的 onagent**——依 slug 動態載入對應業主的 onagent API key，把 `useDemoChat.ts` 換成真正嵌入 `@onagent/bridge`（介面 `started`/`messages`/`isTyping`/`start`/`send` 已經對應好「使用者按下開始對話才連線」的時機點，比照 onagent `apps/landing/showcase/src/cases/support` 的 demo 模式，但這次要做成真正依資料動態決定內容）。目前 `mockData.ts` 內建兩筆假業主資料（`chenguang-bakery`、`furry-studio`），串接後要換成打 ai-support 後端查 slug。
4. **`apps/landing`（行銷首頁）從佔位內容開始寫**——目前 `index.html` 只有 `<h1>ai-support</h1>`，實際文案、視覺（清新明亮/乾淨/親民卡通感）、業主端的註冊 CTA 都還沒做。
5. **`business.SetOnagentApp` 目前沒有任何呼叫端**——要等 `onagentclient.CreateApp` 實作完成、`console.go` 的 `createBusiness` 補上呼叫之後才會被用到，屬於預期中的暫時死碼,不是遺漏。
6. **實際跑一次 `go run ./cmd/server` 並用真實 Postgres 驗證**——目前只驗證過 `go build`/`go vet` 靜態編譯通過，**完全沒有實際執行測試過**，包含 schema 是否真的能在乾淨資料庫上建立成功、registration/login API 是否真的能跑通。
7. **`apps/console`、`apps/support` 兩邊都還沒實際裝過 `npm install` 之外的 lint/測試**——`vitest` 已在 `package.json` 裡但目前沒有任何 `*.test.tsx`；`npx tsc -b` 與 `npx vite build` 這次都跑過且通過，但沒有實機瀏覽器視覺驗證（環境內沒有可用的 headless browser/screenshot 工具）。
