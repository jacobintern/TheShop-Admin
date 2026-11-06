# TheShop-Admin

TheShop 電商系統的後台管理介面。單一 Go 專案，server-rendered 頁面（Gin + `html/template`）加 htmx 做局部更新，不需要前端建置工具。

## 功能

- 管理員登入／登出（帳號存在後台自己的 SQLite，不用 backend 的會員帳號）
- 訂單：列表（可依狀態篩選）、單筆檢視、狀態流轉 `pending → paid → shipped → completed`，`pending` 可取消
- 商品：列表（含下架）、新增、修改（價格／庫存／規格／圖片）、上架／下架（軟刪）
- 管理員帳號：新增、刪除、改自己的密碼

商品與訂單資料都在 `TheShop-Backend`，後台透過 `/api/admin/*` 操作，請求帶 header `X-Admin-Key`。

## 與 backend 的契約

| 用途 | API |
| --- | --- |
| 訂單列表 | `GET /api/admin/order/list?page=&fetch=&status=` |
| 單筆訂單 | `GET /api/admin/order/:id` |
| 訂單狀態 | `PUT /api/admin/order/:id/status` body `{status}` |
| 商品列表（含下架） | `GET /api/admin/product/list?page=&fetch=` |
| 單筆商品 | `GET /api/admin/product/:id` |
| 新增商品 | `POST /api/admin/product` → `{id}` |
| 修改商品 | `PUT /api/admin/product/:id`（後台一律帶完整 images 陣列整組替換，空陣列＝清空） |
| 上架／下架 | `PUT /api/admin/product/:id/status` body `{status: 0|1}` |

回應格式 `{code, msg, data}`，`code` 0 成功；錯誤碼對應寫在 `internal/backend/client.go`。

## 環境變數

| 變數 | 預設 | 說明 |
| --- | --- | --- |
| `ADMIN_PORT` | `8081` | 監聽 port |
| `BACKEND_URL` | `http://127.0.0.1:8080` | backend 位址 |
| `ADMIN_API_KEY` | （必填） | 與 backend `ADMIN_APIKEY` 相同，送 `X-Admin-Key` |
| `ADMIN_INIT_ACCOUNT` | `admin` | 第一次啟動時建立的管理員帳號 |
| `ADMIN_INIT_PASSWORD` | （首次啟動必填） | 第一次啟動時的管理員密碼；SQLite 已有帳號後不再使用 |
| `ADMIN_DB_PATH` | `./data/admin.db` | SQLite 檔案路徑；Docker 內為 `/app/data/admin.db` |
| `ADMIN_SESSION_HOURS` | `12` | 登入 session 有效時數 |
| `ADMIN_COOKIE_SECURE` | `false` | HTTPS 時設 `true` |
| `ADMIN_ASSET_DIR` | `./asset` | 字型等靜態資產目錄 |

範例見 `.env.example`。

## 本機啟動

```bash
cp .env.example .env   # 填 ADMIN_API_KEY、ADMIN_INIT_PASSWORD
set -a; source .env; set +a
go run ./cmd/admin
```

開 <http://localhost:8081>，用初始帳密登入。backend 要先跑在 `BACKEND_URL`，且 `/api/admin/*` 已上線，否則訂單／商品頁會顯示連線錯誤（登入與管理員頁不受影響）。

## 測試

```bash
go vet ./... && go test ./...
```

`internal/web` 的測試用假 backend 走完登入、訂單狀態流轉、商品新增／編輯／下架、帳號管理的整頁流程。

## Docker

```bash
docker build -t theshop-admin .
docker run --rm -p 8081:8081 \
  -e BACKEND_URL=http://theshop-backend:8080 \
  -e ADMIN_API_KEY=... -e ADMIN_INIT_PASSWORD=... \
  -v theshop_admin_data:/app/data \
  theshop-admin
```

映像為 multi-stage（golang:1.26-alpine build → alpine），`/app/data` 是 SQLite volume，`EXPOSE 8081`。

## 專案結構

- `cmd/admin`：進入點，載入設定、seed 初始管理員、啟動 HTTP server
- `internal/config`：環境變數
- `internal/store`：SQLite（管理員帳號、session）
- `internal/backend`：backend 管理 API client 與契約型別
- `internal/web`：router、handler、模板（`templates/`）、靜態檔（`static/`，含 htmx）
- `asset/font`：介面字型（jf open 粉圓）
