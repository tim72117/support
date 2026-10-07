# GCP Workload Identity Federation 部署踩坑記錄（2026-10-07）

狀態：**根因已確認，等待最終部署成功驗證。** 記錄這次設定 `wenbi-prod`（ai-support 的 Cloud Run 部署）時，從 GitHub Actions 推送 Docker image 到 Artifact Registry 一直失敗的完整排查過程，供之後建立新 GCP 專案、接新的 CI/CD 時參考——**這個坑在新專案上幾乎一定會重複踩到**，因為它不是程式碼或 workflow 寫法的問題，是「新建的 GCP 專案預設沒啟用某些基礎 API」。

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

## 相關檔案

- `.github/workflows/deploy-cloudrun.yml` — 最終清理回的簡潔版本，只用 `auth@v2`
- 對照範本：`/Users/caitingyu/Documents/tripace/.github/workflows/deploy-cloudrun.yml`（同帳號下已知能正常運作的專案，沒有任何 `setup-gcloud`/手動登入的額外步驟）
