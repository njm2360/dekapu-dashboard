# VRでかプ 統計ダッシュボード

<img width="1920" height="3079" alt="image" src="https://github.com/user-attachments/assets/e1ade4b6-88c1-4132-95a8-bffab90f29b4" />

## 前提条件

- [Git](https://git-scm.com/)がインストールされていること
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) がインストールされていること

## リポジトリの取得

```bash
git clone --recursive https://github.com/njm2360/dekapu-dashboard.git
```

- Git submoduleを使用しているため`--recursive`オプションが必要です。必ず付与して実行してください。

## 環境変数の設定

`.env.template` をコピーして `.env` を作成し、値を編集してください。

```bash
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

```bash
docker compose up -d
```

- 実行時に`no configuration file provided`と表示されて起動できない場合はディレクトリが移動できていません。`cd dekapu-dashboard`でディレクトリを移動してから実行してください。

### 自動起動について

Windows環境でDocker Desktopを使用している場合は、以下の設定にチェックを入れてください。

Settings -> General -> Start Docker Desktop when you sign in to your computer

この設定を有効にした上で、一度だけ`docker compose up -d` を実行しておけば、以降はWindows起動時にDocker Desktopとともにコンテナも自動起動します。**毎回起動コマンドを実行する必要はありません**。

起動後:

- Grafana → [http://localhost:3000](http://localhost:3000) （初期ユーザー: `admin` / `password`）
- InfluxDB → [http://localhost:8086](http://localhost:8086)  （初期ユーザー: `admin` / `password`）

Grafanaを開いてDashboard内に`でかプ実績`という名前のダッシュボードがあります。
起動後1時間程度はデータ不足のため一部のパネルが`No data`と表示される場合があります。
データが全く表示されない場合はダッシュボード左上のユーザー名が入力されていることを確認してください。

## 停止方法

```bash
docker compose stop
```

## 更新方法

```bash
git pull
docker compose down
docker compose up -d --build
```
