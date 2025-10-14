# VRでかプ 統計ダッシュボード

<img width="1920" height="3345" alt="image" src="https://github.com/user-attachments/assets/b63bd627-4199-4ac4-982c-750c161fdcb4" />

## インストール方法

以下のコマンドを`ファイル名を指定して実行`に貼り付けて実行してください。(スタートボタンを右クリック -> `ファイル名を指定して実行`)

```bash
powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/njm2360/dekapu-dashboard/main/install.ps1 | iex
```

【注意】

- 自動で再起動が行われる場合がありますのでインストール前に作業中のアプリケーションは終了しておいてください。
- インストールはパソコンのスペックやインターネット速度によって時間がかかります。ご了承ください。
- 実行時に管理者権限を要求するUAC許可ダイアログが出ますので許可してください。
- Docker起動時にアカウントを求められますがスキップして問題ありません。

## 使用方法

基本的にウェブブラウザからアクセスして使用します。

- Grafana → [http://localhost:3000](http://localhost:3000) （初期ユーザー: `admin` / `password`）
- InfluxDB → [http://localhost:8086](http://localhost:8086)  （初期ユーザー: `admin` / `password`）

Grafanaを開いてDashboard内に`でかプ実績`という名前のダッシュボードがあります。
起動後1日程度はデータ不足のため一部のパネルが`No data`と表示される場合があります。
データが全く表示されない場合はダッシュボード左上のユーザー名が正しく選択されていることを確認してください。

【注意】初回のコンテナ起動時はサービスが立ち上がるまで時間がかかる場合があります。アクセスできない場合は時間をおいて再度お試してください。

## インストール方法（手動導入）

基本的に手動導入をする必要はありませんが必要に応じて手動導入をすることも可能です。

より詳細な手順説明は[こちら](https://github.com/njm2360/dekapu-dashboard/blob/main/README-easy.md)にあります。

### 前提条件

- [Git](https://git-scm.com/)がインストールされていること
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) がインストールされていること

### リポジトリの取得

```bash
git clone https://github.com/njm2360/dekapu-dashboard.git
```

### 環境変数の設定

`.env.template` をコピーして `.env` を作成し、値を編集してください。

```bash
cd dekapu-dashboard
copy .env.template .env
```

設定が必要なのは以下の設定です。`{ユーザー名}`は実際のPCのユーザープロファイルパスの名前に合わせてください。

```ini
VRCHAT_LOG_DIR=/host_mnt/c/Users/{ユーザー名}/AppData/LocalLow/VRChat/VRChat
```

またGrafanaやInfluxDBの初期ユーザー名やパスワード、トークンが記載されています。
必要に応じてデフォルトから変更してください。

`INFLUXDB_BUCKET`は変更しないでください。ダッシュボードが壊れます。

### 起動方法

```bash
docker compose up -d
```

#### 自動起動について

Windows環境でDocker Desktopを使用している場合は、以下の設定にチェックを入れてください。

Settings -> General -> Start Docker Desktop when you sign in to your computer

この設定を有効にした上で、一度だけ`docker compose up -d` を実行しておけば、以降はWindows起動時にDocker Desktopとともにコンテナも自動起動します。**毎回起動コマンドを実行する必要はありません**。

## 停止方法

```bash
docker compose stop
```

## 更新方法

デフォルトではユーザープロファイルの直下に`dekapu-dashboard`フォルダとしてクローンされます。
あらかじめ`cd %USERPROFILE%/dekapu-dashboard`でフォルダを移動してから操作してください。

```bash
git pull
docker compose down
docker compose up -d --build
```

## ダッシュボードの改造について

ダッシュボードの中身は自由にカスタマイズ可能です。ただし初期で用意されているダッシュボードはプロビジョニングで自動配置されたもののため、変更を保存できません。

ダッシュボードの改造をする場合はプロビジョニングされたダッシュボード開き、右上の`Edit`をクリックしたら`Save dashboard`ボタンの右の下矢印から`Save as copy`をクリックして複製してください。

注意: ユーザーで改造したダッシュボードは自動配置されたダッシュボードとは別扱いとなるため、`git pull`を使用した更新の対象外になりますのでご了承ください。
