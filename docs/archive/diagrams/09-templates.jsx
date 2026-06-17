window.Templates = () => (
    <section className="doc" id="templates">
        <div className="kicker">Configuration</div>
        <h1 className="docnum"><span className="n">09</span>conf.toml — 設定リファレンス</h1>
        <h2 className="docsub">すべての設定項目の説明と推奨値</h2>

        <div className="variant-label">
            <span className="v">Top Level</span>
            <h3>グローバル設定</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>キー</th><th>型</th><th>説明</th><th>例</th></tr></thead>
                <tbody>
                    <tr><td><code>tplPreset</code></td><td>string</td><td>テンプレートプリセット名</td><td>"rich", "simple"</td></tr>
                    <tr><td><code>ignoreMessagesDaysOld</code></td><td>int</td><td>この日数より古いタスクをスキップ</td><td>7</td></tr>
                    <tr><td><code>silentUpdateDB</code></td><td>bool</td><td>DB だけ更新（Discord 通知なし）</td><td>false</td></tr>
                    <tr><td><code>threads</code></td><td>int</td><td>並列処理スレッド数</td><td>4</td></tr>
                    <tr><td><code>debug</code></td><td>bool</td><td>デバッグログを有効化</td><td>false</td></tr>
                    <tr><td><code>log</code></td><td>bool</td><td>詳細ログを有効化</td><td>true</td></tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">[kitsu]</span>
            <h3>Kitsu 接続設定</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>キー</th><th>説明</th><th>例</th></tr></thead>
                <tbody>
                    <tr><td><code>hostname</code></td><td>Kitsu サーバーの URL</td><td>"https://kitsu.yourstudio.com"</td></tr>
                    <tr><td><code>email</code></td><td>Bot 用 Kitsu アカウントのメールアドレス</td><td>"bot@yourstudio.com"</td></tr>
                    <tr><td><code>password</code></td><td>Kitsu パスワード（環境変数推奨）</td><td><code>${"{KITSU_PASSWORD}"}</code></td></tr>
                    <tr><td><code>skipComments</code></td><td>true にするとコメントを取得しない</td><td>false</td></tr>
                    <tr><td><code>requestInterval</code></td><td>ポーリング間隔（秒）</td><td>60</td></tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">[discord]</span>
            <h3>Discord 接続設定</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>キー</th><th>説明</th><th>例</th></tr></thead>
                <tbody>
                    <tr><td><code>webhookURL</code></td><td>グローバル Webhook URL（フォールバック）</td><td>"https://discord.com/api/webhooks/..."</td></tr>
                    <tr><td><code>botToken</code></td><td>Discord Bot トークン</td><td><code>${"{DISCORD_BOT_TOKEN}"}</code></td></tr>
                    <tr><td><code>guildID</code></td><td>Discord サーバー（ギルド）ID</td><td>"123456789012345678"</td></tr>
                    <tr><td><code>useThreads</code></td><td>タスクごとにスレッドを作成</td><td>true</td></tr>
                    <tr><td><code>embedsPerRequests</code></td><td>1リクエスト当たりの Embed 数上限</td><td>10</td></tr>
                    <tr><td><code>requestsPerMinute</code></td><td>1分当たりのリクエスト数上限</td><td>30</td></tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">[[discord.productions]]</span>
            <h3>プロジェクト別 Webhook</h3>
        </div>
        <div className="wf box" style={{fontFamily:'var(--mono)',fontSize:'12px'}}>
            <pre style={{margin:0,lineHeight:'1.7'}}>{`[[discord.productions]]
projectName = "FILM_2024"
webhookURL  = "https://discord.com/api/webhooks/..."

[[discord.taskTypeWebhooks]]
taskType   = "Compositing"
webhookURL = "https://discord.com/api/webhooks/..."`}</pre>
        </div>

        <div className="variant-label">
            <span className="v">[googleDrive]</span>
            <h3>Google Drive / ストレージ</h3>
        </div>
        <div className="wf wf-pad box">
            <p style={{margin:0,fontSize:'12px'}}><code>googleDrive.url</code>: プロジェクト共有フォルダの URL。Embed フィールドに「ファイル」リンクとして表示される。</p>
        </div>
    </section>
);
