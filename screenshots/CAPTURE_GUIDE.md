# スクリーンショット撮影ガイド

このドキュメントは、v0.1.0 公開リリース用スクリーンショットを撮影するための手順を説明しています。

## 前提条件

- ローカルマシンで KitsuSync が起動していること（`docker compose up -d`）
- ブラウザが開いていて、`http://localhost:8090` にアクセス可能なこと
- `.env.local` に有効な Kitsu/Discord 認証情報が設定されていること

## 撮影手順

### 1. ログインページ (`login.png`)

**URL:** `http://localhost:8090/bot/login`

**画面内容:**
- KitsuSync のロゴ
- "Kitsu Manager/Admin としてサインイン" フォーム
- Kitsu Email / Password 入力欄

**撮影時の注意:**
- パスワードは空欄のままで OK
- デスクトップ幅（1920x1080 推奨）で撮影

**保存先:** `screenshots/login.png`

---

### 2. セットアップウィザード Step 1: Kitsu Connection (`wizard-step1.png`)

**事前準備:**
1. `/bot/login` でログイン（Kitsu マネージャー/管理者アカウント）
2. `/bot/setup-wizard` にアクセス

**URL:** `http://localhost:8090/bot/setup-wizard` (Step 1 が表示されている状態)

**画面内容:**
- "Step 1: Kitsu Connection" ヘッダ
- Kitsu Hostname / Email / Password フォーム
- "Checking connection..." または "✓ Kitsu: Authenticated" バッジ

**撮影時の注意:**
- パスワードフィールドは空欄のままで OK（シークレット）
- ✓ Authenticated バッジが表示されている状態で撮影

**保存先:** `screenshots/wizard-step1.png`

---

### 3. セットアップウィザード Step 2: Discord Bot (`wizard-step2.png`)

**手順:** Step 1 で "Next" をクリック

**URL:** `http://localhost:8090/bot/setup-wizard` (Step 2 が表示されている状態)

**画面内容:**
- "Step 2: Discord Bot" ヘッダ
- Bot Token / Guild ID フォーム
- "✓ Discord: BotName / ServerName" バッジ

**撮影時の注意:**
- Bot Token フィールドは空欄のままで OK（トークンを入力しないこと）
- ✓ Discord バッジが見える状態で撮影

**保存先:** `screenshots/wizard-step2.png`

---

### 4. セットアップウィザード Step 3: Project Setup (`wizard-step3.png`)

**手順:** Step 2 で "Next" をクリック

**URL:** `http://localhost:8090/bot/setup-wizard` (Step 3 が表示されている状態)

**画面内容:**
- "Step 3: Project Setup" ヘッダ
- Project ドロップダウン（Kitsu から取得したプロジェクト一覧）
- Language selector (Japanese / English)
- "Create Channels" ボタン
- 作成済みプロジェクトリスト（すでに設定されている場合）

**撮影時の注意:**
- 複数プロジェクトが表示される場合、どれか1つを選択した状態で撮影
- "Create Channels" ボタンの状態（enabled / disabled）が見える状態

**保存先:** `screenshots/wizard-step3.png`

---

### 5. 管理ダッシュボード (`admin-dashboard.png`)

**手順:**
1. ウィザードを完了するか、既存セッションから `/bot/admin` にアクセス
2. 左側のナビゲーションから "Dashboard" を選択

**URL:** `http://localhost:8090/bot/admin`

**画面内容:**
- システムステータス（Poller running, Last sync 等）
- Active Projects カード（プロジェクト名、チャンネル数、Webhook 数）
- Warnings セクション（ある場合）

**撮影時の注意:**
- プロジェクト名・チャンネル名は非公開スタジオ名でも可（GH では匿名化できる）
- Webhook URLs は見えないことを確認
- デスクトップ幅で撮影、スクロール後の内容も含める場合は複数ショットを連結

**保存先:** `screenshots/admin-dashboard.png`

---

### 6. Discord 通知例 (`discord-notification.png`)

**取得方法 A: 実際のテストメッセージから**

1. Kitsu で任意のタスクのステータスを変更（例：`TODO` → `WFA`）
2. Discord のアサイン済みチャンネルで通知メッセージが届く
3. メッセージ内容をスクリーンショット撮影

**取得方法 B: 履歴から**

1. 過去に送信された KitsuSync 通知メッセージをスクロール検索
2. 見出し（タイトル）、ステータス、変更者、Drive リンク、KITSU で開くボタンが見える状態で撮影

**画面内容:**
- **タイトル**: `グループ名 / ショット名 - タスク名` (クリッカブルなリンク)
- **本文**: ステータス絵文字 + **太字ステータス** + コメントアイコン (optional)
- **変更者**: 作成・変更した人の名前
- **コメント**: あれば 💬 アイコン付き
- **リンク**: [📁 Drive] リンク + **[🦊 KITSU で開く →]** ボタン

**撮影時の注意:**
- Webhook URL や内部 ID は見えないこと
- タスク名は非公開スタジオ名でも可
- モバイルでの見え方が気になる場合は、モバイル幅（375px）でも撮影

**保存先:** `screenshots/discord-notification.png`

---

## 全体のトーン・スタイル

- **背景**: ダークテーマ（v0.1.0 デフォルト）
- **表示言語**: 日本語 OK（英語切り替え可能）
- **プライバシー**: スタジオ名・ユーザー名は非公開情報でも公開用に匿名化不要（GitHub で削除できる）

## スクリーンショット保存後

撮影が完了したら：

```bash
ls -la screenshots/
# 以下ファイルが存在すること:
# - login.png
# - wizard-step1.png
# - wizard-step2.png
# - wizard-step3.png
# - admin-dashboard.png
# - discord-notification.png

git add screenshots/
git commit -m "docs: add v0.1.0 release screenshots"
```

## トラブルシューティング

| 問題 | 対処法 |
|------|--------|
| Step 2 で "✓ Discord" バッジが表示されない | `.env.local` の Bot Token と Guild ID が正しいか確認 |
| Step 3 で Project ドロップダウンが空 | Kitsu に認証情報で接続できているか確認；`docker compose logs app \| grep "Got tasks"` で確認 |
| Discord 通知が見つからない | チャンネルが実際に作成されているか確認；ウィザード完了直後は 1-2 ポーリングサイクル待ってから確認 |
| スクリーンショット撮影時にパスワードが見える | ブラウザの開発者ツール（F12）で HTML の value 属性を削除するか、入力フィールドが空になるまで待つ |

---

## 次のステップ

スクリーンショット撮影完了後：

1. GitHub で PR を作成
2. `screenshots/` ディレクトリ内のファイルを確認
3. README.md の "Preview Images" セクションでスクリーンショット URL を参照できるか確認
4. v0.1.0 リリースへ
