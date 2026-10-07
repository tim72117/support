# ai-support 後端容器映像。
# build context 為「專案根目錄」(docker build -f backend/Dockerfile .),
# COPY 路徑相對根目錄寫(backend/...、apps/...)。
#
# 四個獨立 Vite 專案(apps/landing、apps/console、apps/support、apps/admin)
# 各自 build 後複製進 backend/cmd/server/{landing,console,support,admin}/dist/,
# 對應 cmd/server/static_landing.go、static_console.go、static_support.go、
# static_admin.go 的 go:embed ——路徑名稱必須完全一致,否則 embed 到的只會是
# checked-in 的 placeholder index.html(見那四個 dist/ 目錄底下的說明;模式
# 照抄 /Users/caitingyu/Documents/tripace/Dockerfile 的多前端合併編譯作法)。
#
# 四個前端各自掛載在哪個路徑前綴,見 cmd/server/main.go 掛 mux 的那幾行與
# 各自 static_*.go 開頭的說明:
#   apps/landing  -> "/"         (對外行銷首頁,純靜態多頁面,無 SPA router)
#   apps/console   -> "/app/"    ("/console/" 已經是後端 API 前綴,見
#                                  main.go mountCredentialedRoutes,故 console
#                                  前端故意不掛在 "/console/" 底下)
#   apps/support   -> "/support/" (前端本來就假設自己活在這個路徑下,見
#                                  apps/support/src/App.tsx 的 slugFromLocation)
#   apps/admin     -> "/admin/"   (平台管理後台,ADMIN_EMAILS 白名單帳號專用)
#
# 建置(從專案根目錄):
#   docker build -f backend/Dockerfile -t ai-support-server .
# 本機跑(env 由 --env-file 注入,不會把 .env 烤進映像):
#   docker run --rm -p 8080:8080 --env-file backend/.env ai-support-server

# ---- 階段 1:build apps/landing(純靜態多頁行銷站,無 build-time 環境變數) ----
# 見 apps/landing/assets/site.js:console/api 連結是用 URL query string
# (?console=...&api=...)在「執行期」(瀏覽器載入後)決定的,不是 Vite
# build 時期寫死進 bundle,故這裡不需要任何 --build-arg / ARG。
FROM node:22-alpine AS landing-build
WORKDIR /landing
COPY apps/landing/package.json apps/landing/package-lock.json ./
RUN npm ci
COPY apps/landing/ ./
RUN npm run build

# ---- 階段 2:build apps/console(業主管理後台) ----
# VITE_API_BASE / VITE_SUPPORT_URL 是 Vite build 時期讀取並寫死進 bundle
# 的環境變數(見 apps/console/src/api.ts、BusinessEditor.tsx),故用 ARG
# 轉 ENV 讓 npm run build 讀得到。兩者都不是機密資料(都只是後端/consumer
# 頁面的公開網址),不需要走 Secret Manager,直接用一般 build-arg 傳入,
# 由 deploy-cloudrun.yml 決定正式環境的值(通常就是同一個 Cloud Run
# 服務自己的網址 + "/support" 字尾,與 PUBLIC_ALLOWED_ORIGIN 的值相呼應)。
FROM node:22-alpine AS console-build
WORKDIR /console
COPY apps/console/package.json apps/console/package-lock.json ./
RUN npm ci
COPY apps/console/ ./
ARG VITE_API_BASE
ENV VITE_API_BASE=${VITE_API_BASE}
ARG VITE_SUPPORT_URL
ENV VITE_SUPPORT_URL=${VITE_SUPPORT_URL}
RUN npm run build

# ---- 階段 3:build apps/support(消費端對話頁) ----
# VITE_API_BASE 同上,見 apps/support/src/api.ts。
FROM node:22-alpine AS support-build
WORKDIR /support
COPY apps/support/package.json apps/support/package-lock.json ./
RUN npm ci
COPY apps/support/ ./
ARG VITE_API_BASE
ENV VITE_API_BASE=${VITE_API_BASE}
RUN npm run build

# ---- 階段 4:build apps/admin(平台管理後台,ADMIN_EMAILS 白名單帳號專用) ----
# VITE_API_BASE 同 console/support(見 apps/admin/src/api.ts)。
FROM node:22-alpine AS admin-build
WORKDIR /admin
COPY apps/admin/package.json apps/admin/package-lock.json ./
RUN npm ci
COPY apps/admin/ ./
ARG VITE_API_BASE
ENV VITE_API_BASE=${VITE_API_BASE}
RUN npm run build

# ---- 階段 5:編譯 Go ----
FROM golang:1.26 AS build

# 先單獨複製 go.mod / go.sum 以利 layer 快取(相依沒變時不重抓)。
COPY backend/go.mod backend/go.sum /src/backend/
RUN cd /src/backend && go mod download

# 再複製完整源碼。
COPY backend/ /src/backend/

# 把四個前端 dist 放到各自的 embed 路徑。用 rm -rf 先清掉 checked-in 的
# placeholder index.html,避免殘留檔案混進真正的 build 產物(見
# cmd/server/{landing,console,support,admin}/dist/index.html 的說明)。
RUN rm -rf \
    /src/backend/cmd/server/landing/dist/* \
    /src/backend/cmd/server/console/dist/* \
    /src/backend/cmd/server/support/dist/* \
    /src/backend/cmd/server/admin/dist/*
COPY --from=landing-build /landing/dist/. /src/backend/cmd/server/landing/dist/
COPY --from=console-build /console/dist/. /src/backend/cmd/server/console/dist/
COPY --from=support-build /support/dist/. /src/backend/cmd/server/support/dist/
COPY --from=admin-build /admin/dist/. /src/backend/cmd/server/admin/dist/

# 靜態編譯:關 CGO 產出不依賴 libc 的單一執行檔,可放進極小的 base image。
RUN cd /src/backend && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./cmd/server

# ---- 階段 6:執行 ----
# distroless:只含執行檔需要的最小 runtime,無 shell、體積小、攻擊面小。
# 內含 CA 憑證,連 Postgres(sslmode=require,例如 Cloud SQL)的 TLS 才驗得過。
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server

# Cloud Run 會注入 PORT(預設 8080);main.go 在 ADDR 未設定時改讀 PORT
# (見 cmd/server/main.go 的說明)。
EXPOSE 8080
ENTRYPOINT ["/app/server"]
