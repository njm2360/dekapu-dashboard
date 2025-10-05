# 導入のポイント

## はじめに

GitとDockerがインストールされていることをチェックしてください

- [Git](https://git-scm.com/)
- [Docker](https://www.docker.com/products/docker-desktop/)

Windowsのアップデートも忘れずに行ってください

### インストールについて

- Git
  - インストール時にいろいろ聞かれますが基本的にすべてそのままでNextをクリックしていけばOKです。
- Docker
  - 何も設定は弄らずにそのままNextでインストールすればOKです。
  - もしかしたら再起動が求められるかもしれません。その場合は再起動してください。インストール中や後にUAC(ユーザーアカウント制御)が出る場合がありますがこちらは許可してOKです。
  - インストール後にDocker Desktopが立ち上がってアカウントを求められますがアカウント無しで使用可能です。

## 基礎知識

このツールの導入ではターミナルを使用してコマンドを入力することでインストールを行います。

- ターミナルの起動方法
    1. スタートメニューを開く
    2. `power`と入力して検索結果に`Windows PowerShell`があるのでクリックして開く
    3. 黒い画面が出てきたらOKです

ターミナルでは現在の場所（カレントディレクトリ）が表示されています。
ここが間違っていると正しく導入できないので注してください。普通に起動した場合はカレントディレクトリとして自分のユーザー名の直下にいると思います。（例: `C:\Users\10576>`）
ここが間違っていると誤った場所に導入してしまうので、もし違っている場合（管理者で起動した場合`C:\Windows\System32>`になっている場合があります）は`cd %homepath%`を実行してユーザーフォルダに移動してください。

## 導入方法

1. Gitからリポジトリをクローンします
    正しいカレントディレクトリにいることを確認して以下のコマンドでクローンして下さい

    `git clone https://github.com/njm2360/dekapu-dashboard.git`

    以下のような出力が出れば成功です

    ```bash
    PS C:\Users\10576> git clone https://github.com/njm2360/dekapu-dashboard.git
    Cloning into 'dekapu-dashboard'...
    remote: Enumerating objects: 175, done.
    remote: Counting objects: 100% (175/175), done.
    remote: Compressing objects: 100% (122/122), done.
    remote: Total 175 (delta 63), reused 139 (delta 35), pack-reused 0 (from 0)
    Receiving objects: 100% (175/175), 62.02 KiB | 6.20 MiB/s, done.
    Resolving deltas: 100% (63/63), done.
    ```

1. クローンしたリポジトリの中に移動します

    `cd dekapu-dashboard`

1. 環境変数を設定します

    `(Get-Content .env.template) -replace '^USERNAME=.*', "USERNAME=$env:USERNAME" | Set-Content .env`

1. Dockerコンテナを起動します

    `docker compose up -d`

    以下のような出力であれば正常です

    ```bash
    PS C:\Users\10576\dekapu-dashboard> docker compose up -d
    [+] Running 3/3
     ✔ Container influxdb    Running                                                                                                                                                                 0.0s
     ✔ Container grafana     Running                                                                                                                                                                 0.0s
     ✔ Container log-parser  Running
    ```

    - ネットワークの速度やPCのスペックによっては少し時間がかかります。

    - 起動時にWindows Defender Firewallの画面が出てくることがありますが、基本的に許可してOKです（LAN内の別の端末からダッシュボードを閲覧するためにも必要）

    - 以下のようなメッセージが出た場合はDocker Desktopが起動していません。Docker Desktopを起動してから再度試してください。

     ```bash
     unable to get image 'grafana/grafana:12.1.1': error during connect: Get "http://%2F%2F.%2Fpipe%2FdockerDesktopLinuxEngine/v1.51/images/grafana/grafana:12.1.1/json": open //./pipe/dockerDesktopLinuxEngine: The system cannot find the file specified.
     ```

1. VRChatを起動してでかプに行きます

1. ダッシュボードにアクセスします

    ブラウザで以下のURLにアクセスしてください

    [http://localhost:3000](http://localhost:3000)

    - 初期のユーザ名は`admin`、パスワードは`password`です。
    - ログイン後にダッシュボードに`でかプ実績`という名前のダッシュボードがあります。それを開いて値が出てこればOKです。
    - 導入してから1時間から1日程度は一部のパネルが正常に表示されない場合がありますのでデータが貯まるまでお待ち下さい。

1. Docker の設定

    初期設定だとWindowsが起動した際にDockerが自動で起動しません。

    `Settings -> General -> Start Docker Desktop when you sign in to your computer`

    にチェックを入れることでPCを再起動した後も自動で立ち上がるようになりますのでこの設定を必ず入れてください。
