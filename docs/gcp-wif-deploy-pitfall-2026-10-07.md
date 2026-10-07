# GCP Cloud Run 部署踩坑記錄（2026-10-07）

狀態：**全部解決，部署已成功，domain mapping 已建立。** 記錄這次把 ai-support 部署到全新 GCP 專案 `wenbi-prod`（Cloud Run + Workload Identity Federation）時，從第一次觸發部署到真正建立 domain mapping、服務開始回應，中間連續踩過的 **4 個獨立的坑**。每一個都是部署路上完全不同的環節，解決一個才會暴露下一個——供之後建立新 GCP 專案、接新的 CI/CD 時按順序檢查，可以一次避開全部，不用重新一個一個踩。

## 全部四個坑總覽

| # | 階段 | 症狀 | 根因 |
|---|---|---|---|
| 1 | `docker push` | `Unauthenticated request` | 新專案沒啟用 WIF 依賴的基礎 API（`iamcredentials`、`cloudresourcemanager`、`cloudbuild`、`iam`） |
| 2 | `gcloud run deploy` | `Permission denied on secret ... for Revision service account` | Cloud Run **執行期**用的 service account（GCP 預設的 compute SA）跟**部署用**的 WIF service account 是不同身份，沒被授權讀 Secret Manager |
| 3 | `gcloud run deploy` | `Secret ... was not found`（`versions/latest`） | 用空字串建立 Secret Manager 密鑰時，`gcloud secrets create` 不會報錯，但**不會真的產生任何版本**——Secret Manager 本身拒絕空內容 |
| 4 | 容器啟動後 | `container failed to start and listen on the port` | 背景 goroutine 對 `nil`（連不上）的 `*gorm.DB` 呼叫方法，gorm 對 nil receiver 是 panic 不是回傳 error，整個 process 直接 crash |

下面依發現順序詳細記錄每一個。

## 症狀

GitHub Actions 的 `Push image` 步驟（`docker push` 到 Artifact Registry）持續失敗，錯誤訊息固定是：

```
denied: Unauthenticated request. Unauthenticated requests do not have permission
"artifactregistry.repositories.uploadArtifacts" on resource
"projects/<PROJECT>/locations/<REGION>/repositories/<REPO>" (or it may not exist)
```

**關鍵誤導點**：這個錯誤訊息讀起來很像「IAM 權限不夠」，導致第一直覺會去檢查/重新授予 `roles/artifactregistry.writer`、重新設定 Workload Identity Federation (WIF) 的 binding、甚至懷疑 secrets 設定錯誤——**這些全部都不是真正原因**，而且會浪費大量時間在錯誤方向上（這次花了連續 9 次部署嘗試、約 40 分鐘才找到真正根因）。

## 真正根因

**新建立的 GCP 專案，預設沒有啟用 WIF 身份交換鏈路背後依賴的幾個基礎 API**：

| API | 用途 |
|---|---|
| `iamcredentials.googleapis.com` | WIF 的核心：用 OIDC token 換取 service account 的 access token，就是透過這個 API |
| `cloudresourcemanager.googleapis.com` | `gcloud` 許多基本操作（例如 `gcloud projects describe`）背後需要 |
| `cloudbuild.googleapis.com` | 部分 gcloud/Docker 整合流程會用到 |
| `iam.googleapis.com` | IAM 相關操作的基礎 API |

只啟用 `run.googleapis.com`、`artifactregistry.googleapis.com`、`secretmanager.googleapis.com`（部署流程表面上用到的那幾個）**完全不夠**——這幾個背景依賴 API 不會因為你用到 Cloud Run/Artifact Registry 就自動被啟用，而且**錯誤訊息完全不會提示你是 API 沒開**，只會顯示成「Unauthenticated」，因為 WIF 的 token 交換在更早的階段就已經失敗，根本沒機會產生一個「身份正確但權限不足」的 403 錯誤。

## 為什麼這麼難找

1. 錯誤訊息（`Unauthenticated` + `uploadArtifacts` 權限字樣）強烈暗示是 IAM 權限問題，不是「缺 API」問題
2. `gcloud auth list` 在 CI 環境裡會顯示「有 active account」，容易誤判成「身份沒問題」——但那只代表 WIF **寫入了憑證檔案**，不代表後續任何一次真正呼叫 API 去交換 token 會成功
3. 每一個缺少的 API，只有在流程**真正呼叫到它**的那一步才會報錯，而不是一次把所有缺失列出來——所以會像剝洋蔥一樣，修一個 API 之後重跑，又在更後面的步驟撞到下一個缺的 API
4. 直接對比「新專案」跟「舊的、已知可運作的專案」（這次是跟 tripace 的 `shuttle-045094509`）啟用的 API 清單，才是真正快速定位的方法——單靠看錯誤訊息猜測，方向容易跑偏

## 繞了哪些彎路（不是根因，記錄下來避免重蹈覆轍）

排查過程中曾經懷疑並嘗試過以下方向，**全部都不是真正問題**：

1. **IAM 權限傳播延遲**——等了超過 15 分鐘，問題依舊，排除
2. **WIF secrets（`WIF_PROVIDER`/`WIF_SERVICE_ACCOUNT`）設定錯誤**——重新設定過，沒有變化
3. **`gcloud` CLI 沒有真正登入**——加了手動 `gcloud auth login --cred-file=...`，結果反而讓狀況更糟：`gcloud auth list` 顯示「有帳號」，但緊接著 `gcloud auth print-access-token` 直接失敗（exit code 1，完全拿不到 token）。手動登入讓 gcloud 進入一個「看起來有帳號、但實際壞掉」的狀態，比什麼都不做更糟
4. **互動式提示卡住**（`gcloud auth login` 問「是否覆蓋現有憑證？」）——加 `--quiet` 解決了這個子問題，但主問題依舊
5. **改用 `google-github-actions/setup-gcloud@v2`** 取代手動 `gcloud auth login`——問題依舊存在，證明這不是「用錯 action」的問題
6. **Service account 缺少對自己的 `roles/iam.serviceAccountTokenCreator` 繫結**——比對 tripace 的設定後發現這條差異並補上，結果**沒有解決問題**（但這條繫結本身不是壞事，可以留著，只是它不是這次的根因）

直到加入一個完全不碰 Docker、純粹測試 `gcloud projects describe` 的最小化除錯步驟，才終於在錯誤訊息裡看到明確的 `SERVICE_DISABLED` / `iamcredentials.googleapis.com has not been used in project ... or it is disabled` 字樣，第一次拿到真正可以採取行動的線索。

## 正確的修復步驟

1. **找一個已知能正常運作的對照專案**（同帳號底下、已經成功部署過 CI/CD 的任何專案），列出它已啟用的 API：
   ```bash
   gcloud services list --enabled --project=<WORKING_PROJECT> --format="value(config.name)" | sort
   ```

2. **列出新專案目前啟用的 API**，比對差異：
   ```bash
   gcloud services list --enabled --project=<NEW_PROJECT> --format="value(config.name)" | sort
   comm -23 <(對照專案清單排序) <(新專案清單排序)
   ```

3. **一次全部啟用缺少的基礎 API**（不要只啟用表面用到的 `run`/`artifactregistry`/`secretmanager`）：
   ```bash
   gcloud services enable \
     iamcredentials.googleapis.com \
     cloudresourcemanager.googleapis.com \
     cloudbuild.googleapis.com \
     iam.googleapis.com \
     --project=<NEW_PROJECT>
   ```

4. **Workflow 本身維持最簡單的寫法**，不需要 `setup-gcloud@v2`、不需要手動 `gcloud auth login`——單純的 `google-github-actions/auth@v2` 就足夠，前提是上面的 API 都已經啟用：
   ```yaml
   - name: Authenticate to Google Cloud
     uses: google-github-actions/auth@v2
     with:
       workload_identity_provider: ${{ secrets.WIF_PROVIDER }}
       service_account: ${{ secrets.WIF_SERVICE_ACCOUNT }}
   ```

## 下次建立新 GCP 專案要接 WIF + CI/CD 時，直接照這份清單啟用

與其等錯誤一個一個浮現，建專案當下就一次啟用：

```bash
gcloud services enable \
  run.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  iamcredentials.googleapis.com \
  cloudresourcemanager.googleapis.com \
  cloudbuild.googleapis.com \
  iam.googleapis.com \
  --project=<NEW_PROJECT>
```

（前三個是部署流程表面上直接用到的；後四個是 WIF 身份交換鏈路背後默默依賴、新專案預設沒開的。）

---

## 坑 2：部署用的身份 ≠ 執行期用的身份

**症狀**（在 `gcloud run deploy` 這步，坑 1 解決、image 成功推送之後才出現）：

```
ERROR: (gcloud.run.deploy) spec.template.spec.containers[0].env[12].value_from.secret_key_ref.name:
Permission denied on secret: projects/<NUM>/secrets/DATABASE_URL/versions/latest
for Revision service account <NUM>-compute@developer.gserviceaccount.com.
The service account used must be granted the 'Secret Manager Secret Accessor' role
```

**根因**：容易搞混的一點——`gcloud run deploy` 這個**部署動作**用的是 WIF 綁定的 service account（這次是 `github-deployer@wenbi-prod.iam.gserviceaccount.com`），但部署完成後，Cloud Run **容器執行期**讀取 `--update-secrets` 掛載的環境變數時，用的是**另一個身份**：GCP 專案的預設 compute service account（`<PROJECT_NUMBER>-compute@developer.gserviceaccount.com`）。這兩個身份完全獨立，授權了前者不會自動讓後者也有權限。

**修復**：

```bash
gcloud projects add-iam-policy-binding <PROJECT> \
  --member="serviceAccount:<PROJECT_NUMBER>-compute@developer.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor" \
  --condition=None
```

---

## 坑 3：Secret Manager 不接受空字串內容

**症狀**（坑 2 解決後，下一次部署出現）：

```
ERROR: (gcloud.run.deploy) spec.template.spec.containers[0].env[13].value_from.secret_key_ref.name:
Secret projects/<NUM>/secrets/GOOGLE_OAUTH_CLIENT_SECRET/versions/latest was not found
```

**根因**：這個專案有幾個選填密鑰（例如 Google OAuth，沒設定就停用該功能），建立對應 Secret 時圖方便用空字串當佔位值：

```bash
echo -n "" | gcloud secrets create GOOGLE_OAUTH_CLIENT_SECRET --project=<PROJECT> --data-file=-
```

**這個指令執行時不會報任何錯誤**，看起來完全成功——但 Secret Manager 拒絕空內容的 payload，實際上**沒有建立任何版本**。`gcloud secrets versions list` 會顯示空清單。等到 `gcloud run deploy` 真的去掛載 `:latest` 版本時，才會發現這個版本根本不存在。

**修復**：Secret 內容不能是空字串，選填的密鑰要嘛別建立 Secret（workflow 的 `--update-secrets` 清單裡也別列它），要嘛塞一個非空的佔位字串（例如 `unset`）讓它有合法版本可以掛載：

```bash
printf 'unset' | gcloud secrets versions add GOOGLE_OAUTH_CLIENT_SECRET --project=<PROJECT> --data-file=-
```

---

## 坑 4：背景 goroutine 對 nil DB 呼叫方法是 panic，不是 error

**背景**：這次部署時 `DATABASE_URL` 刻意先指向一個雲端服務連不到的位址（本機 Postgres），接受「資料庫功能全部失效」以換取先讓服務部署起來、能設定 domain mapping。為此把 `db.Open` 失敗從 `os.Exit(1)` 改成記警告繼續跑。

**症狀**（坑 3 解決、所有環境變數/密鑰都掛載成功後，容器啟動卻還是失敗）：

```
ERROR: (gcloud.run.deploy) The user-provided container failed to start and listen on
the port defined provided by the PORT=8080 environment variable within the allocated timeout.
```

這個錯誤訊息本身完全沒有線索，要去 Cloud Run revision 的實際 log 才看得到：

```bash
gcloud logging read 'resource.type="cloud_run_revision" AND resource.labels.service_name="<SERVICE>" AND resource.labels.revision_name="<REVISION>"' \
  --project=<PROJECT> --limit=30 --format="value(timestamp,textPayload)" --order=asc
```

log 裡才看得到真正原因：

```
panic: runtime error: invalid memory address or nil pointer dereference
gorm.io/gorm.(*DB).Session(0x0, ...)
gorm.io/gorm.(*DB).WithContext(...)
github.com/tim72117/ai-support/internal/billing.(*Service).RenewDue(...)
```

**根因**：改成「`db.Open` 失敗不中斷啟動」只處理了**那一個呼叫點**，完全沒有檢查後面所有假設「資料庫一定連得上」的程式碼。這次踩到的是 `billing.Service.Run()`——一個每小時執行一次的背景 goroutine，它的 `RenewDue()` 對 `gormDB` 呼叫 `WithContext(...)`，而 **gorm 對 `nil` receiver 呼叫方法是直接 panic，不是回傳 error**。這個 goroutine 在程式啟動後幾秒內就會被叫到，整個 process 因此直接 crash（`exit(2)`），不是「某個 API 路由出錯」那種局部失敗。

**修復**：資料庫是 `nil` 時，不要啟動這個背景任務（反正沒資料庫它也做不了任何事）：

```go
case gormDB == nil:
    log.Warn("database not connected; billing's background renewal loop is not started")
default:
    billingSvc = billing.New(gormDB, tappay.New(tpCfg), quotaSvc, log)
    go billingSvc.Run(context.Background(), time.Hour)
```

**重要提醒**：這只修了這次實際撞到的那一個背景任務。「把資料庫從硬依賴改成軟依賴」這件事本質上有風險——任何其他地方只要用同樣的假設寫程式碼（直接對 `*gorm.DB` 呼叫方法、不檢查 nil），都可能是下一個會在執行期才浮現的 panic 點。沒有做過全面審查，這是刻意的技術債，見下方「最終狀態」。

---

## 最終驗證結果（2026-10-07）

全部 4 個坑解決後，部署成功：

- Cloud Run 服務 `ai-support-server` 部署成功，`https://ai-support-server-f3urmeu47a-de.a.run.app` 回應 `HTTP 200`
- Domain mapping 建立成功，指向 `ai.shuttle.tools`（待 DNS 那邊加上 Google 要求的 CNAME `ai → ghs.googlehosted.com.` 才會真正生效）
- `min-instances=0`、`max-instances=1` 維持，成本控制在接近零

## 已知的技術債（部署成功了，但還沒處理）

這次是「先讓服務能跑起來、domain mapping 能設定」優先，刻意跳過了把資料庫換成真正可連線的雲端 Postgres 這件事。目前：

- `DATABASE_URL` 仍指向本機/不可達的位址，**所有資料庫相關功能實際上都是壞的**（登入、業主管理、對話記錄、billing 等）
- `db.Open` 失敗從致命改成警告（`backend/cmd/server/main.go`）——這是 TEMPORARY 標記的改動，之前的 commit message 裡有寫
- `billing.Service.Run()` 在 `gormDB == nil` 時不啟動——同樣 TEMPORARY

**下一步**：建立真正雲端可連線的 Postgres（Cloud SQL 或其他），更新 `DATABASE_URL` 這個 Secret 的內容，然後把上面兩處 TEMPORARY 改動**都要 revert 回去**（資料庫連不上應該讓服務無法啟動，而不是帶著全面壞掉的資料庫功能默默運行——目前這個狀態只適合拿來驗證部署管線本身，不是一個該長期存在的正式環境狀態）。

## 相關檔案

- `.github/workflows/deploy-cloudrun.yml` — 最終清理回的簡潔版本，只用 `auth@v2`
- `backend/cmd/server/main.go` — 兩處 TEMPORARY 標記的改動（db.Open 非致命化、billing 背景任務的 nil 檢查）
- 對照範本：`/Users/caitingyu/Documents/tripace/.github/workflows/deploy-cloudrun.yml`（同帳號下已知能正常運作的專案，沒有任何 `setup-gcloud`/手動登入的額外步驟）
