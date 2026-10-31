# PicPilot Platform

[简体中文](README.md) | [English](README.en.md) | [日本語](README.ja.md)

画像生成、動画生成、EC 制作、参考画像、自由キャンバス、Provider ルーティング、ポイント、決済、運営管理をまとめたオープンソースのマルチユーザー AI プラットフォームです。業務ロジック、タスク、保存は Go、画面は React/Next.js が担当します。

> **状態：公開移行検証版。** 初回デプロイには新しいデータベースを使用し、旧 Java 本番データベースには接続しないでください。

## エディション

| リポジトリ | 用途 |
| --- | --- |
| `suakitsu/picpilot-platform` | 複数ユーザー運営、管理画面、ポイント、決済、リスク管理 |
| `suakitsu/picpilot-personal` | 自分の Provider を使う個人セルフホスト版。課金と管理画面はありません |

どちらの Go リポジトリにも完全なソースと `core` スナップショットが含まれ、1 つのクローンだけでビルドできます。

## クイックスタート

Git、Docker Engine/Docker Desktop、Docker Compose v2 が必要です。初回ビルドは 4 CPU コア、メモリ 8 GB、空きディスク 10 GB 程度を推奨します。

```sh
git clone https://github.com/suakitsu/picpilot-platform.git
cd picpilot-platform
```

Windows PowerShell:

```powershell
./scripts/init.ps1
docker compose up -d --build
docker compose ps
docker compose exec api /picpilot --setup-token
```

Linux / macOS は最初の行を `sh scripts/init.sh` に置き換え、残りは同じです。

1. `http://localhost:8080` を開き、端末に表示された一度だけの初期化トークンで最初の管理者を作成します。
2. `http://localhost:8081` を開いて管理画面にログインします。
3. 実際のプロトコル、モデル、URL、キーを Provider に登録し、確認してから有効化します。

新規インストールに既定アカウントやパスワードはありません。公開登録、決済、Local Worker Provider は初期状態で無効です。`instance-secrets.json` を削除すると保存済み Provider キーを復号できません。

## Provider とローカルゲートウェイ

設定済み Provider の Base URL は `http://` と `https://` の両方に対応し、`http://127.0.0.1:8888/v1` のようなローカルゲートウェイも指定できます。コンテナからホストへ接続する場合、Docker Desktop では `host.docker.internal` を使用します。ユーザーが送信した外部画像 URL は引き続き厳格な安全ダウンロードを通ります。

Provider は上流の実際のプロトコルに対応している必要があります。サービス名だけでは互換性は得られません。設定確認で有料モデルは呼び出しません。

## 画面プレビュー

![運用ダッシュボード](docs/screenshots/platform-admin-dashboard-desktop.png)

![ユーザーワークスペース](docs/screenshots/platform-user-workspace-desktop.png)

![AI 作成画面](docs/screenshots/platform-user-creative-desktop.png)

![履歴画面](docs/screenshots/platform-user-history-desktop.png)

## デプロイと運用

- 既定では `127.0.0.1:8080/8081` のみ公開し、PostgreSQL のホストポートは公開しません。
- 外部公開時はゲートウェイまたはリバースプロキシで HTTPS を設定し、公開 URL を一致させてください。
- `.env`、データベース、アップロードファイル、`instance-secrets.json` をバックアップし、Git には登録しないでください。
- 通常停止は `docker compose stop` を使用します。`docker compose down -v` は永続ボリュームを削除します。

詳細：

- [設定、HTTPS、Local Worker](docs/configuration.md)
- [バックアップ、復元、更新、ロールバック](docs/operations.md)
- [テストと安全境界](docs/testing.md)
- [実装と検証状況](docs/implementation-status.md)

## ライセンス

PicPilot の独自コードは [MIT](LICENSE) です。同梱の Nova データと 2 つのキャンバスは AGPL-3.0 を保持し、組み合わせたアプリケーションは AGPL-3.0 で配布します。範囲と第三者通知は [LICENSING.md](LICENSING.md) を参照してください。
