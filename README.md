# VRでかプ 統計ダッシュボード

<img width="1919" height="933" alt="image" src="https://github.com/user-attachments/assets/7c2e9899-4ac5-4e80-adfd-c1fe94a22fc9" />
<img width="1919" height="897" alt="image" src="https://github.com/user-attachments/assets/1aa65b81-522f-4b15-a262-8b67727a5ba9" />

## 前提条件

- [Git](https://git-scm.com/)がインストールされていること
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) がインストールされていること

## リポジトリの取得

```ps
git clone --recursive https://github.com/njm2360/dekapu-dashboard.git
```

## 環境変数の設定

`.env.template` をコピーして `.env` を作成し、値を編集してください。

```ps
cd dekapu-dashboard
copy .env.template .env
```

主に設定が必要なのは以下の設定です。

- USERNAME: ご使用のPCのユーザー名に置き換えてください。(`C:/Users`以下のフォルダ名です)
- VRCHAT_LOG_DIR: WindowsをCドライブ以外にインストールしている場合は変更してください。（基本的には変更不要です）

```dotenv
# Python app
USERNAME=user
VRCHAT_LOG_DIR=/host_mnt/c/Users/${USERNAME}/AppData/LocalLow/VRChat/VRChat
```

またGrafanaやInfluxDBの初期ユーザー名やパスワード、トークンが記載されています。
必要に応じてデフォルトから変更してください。

`INFLUXDB_BUCKET`は変更しないでください。ダッシュボードが壊れます。

## 起動方法

```ps
docker compose up -d
```

起動後:

- Grafana → [http://localhost:3000](http://localhost:3000) （初期ユーザー: `admin` / `password`）
- InfluxDB → [http://localhost:8086](http://localhost:8086)  （初期ユーザー: `admin` / `password`）

Grafanaを開いてDashboard内に`でかプ実績`という名前のダッシュボードがあります。
起動後1時間程度はデータ不足のため一部のデータが`No data`という表示になる場合があります。
データが全く表示されない場合はダッシュボード左上のユーザー名が入力されている確認してください。

## 停止方法

```bash
docker compose down
```

## 更新方法

```bash
git pull
docker compose down
docker compose up -d --build
```
