window.Threads = () => (
    <section className="doc" id="threads">
        <div className="kicker">Thread System</div>
        <h1 className="docnum"><span className="n">11</span>Thread System &amp; Message Lifecycle</h1>
        <h2 className="docsub">Discord スレッド機能とメッセージ更新・削除の仕組み</h2>

        <div className="variant-label">
            <span className="v">UseThreads</span>
            <h3>スレッドモード（useThreads: true）</h3>
        </div>
        <div className="wf wf-pad box">
            <ol style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li>タスクの<strong>最初の通知</strong>: 新規スレッドを作成（スレッド名 = タスク名）</li>
                <li>DB に <code>discord_thread_id</code> を保存</li>
                <li>同タスクの<strong>次回以降の通知</strong>: 既存スレッドに追記（返信）</li>
                <li>スレッド名は最大 100 文字（超過する場合は切り詰め）</li>
                <li>スレッドが削除・アーカイブされた場合は新規スレッドとして作成</li>
            </ol>
        </div>

        <div className="variant-label">
            <span className="v">Message Update</span>
            <h3>メッセージ更新ロジック</h3>
        </div>
        <div className="wf wf-pad box">
            <div className="box-title">送信先決定フロー</div>
            <ol style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li>DB に <code>discord_message_id</code> が存在する場合 → <code>PATCH /webhooks/&#123;id&#125;/messages/&#123;messageID&#125;</code> で<strong>既存メッセージを更新</strong></li>
                <li>存在しない場合 → <code>POST /webhooks/&#123;id&#125;?wait=true</code> で<strong>新規送信</strong></li>
                <li>送信成功後、レスポンスの message ID を DB に保存</li>
                <li>送信失敗時 → <code>ClearMessageID()</code> で discord_message_id をクリア（次回に重複送信しない）</li>
            </ol>
        </div>

        <div className="variant-label">
            <span className="v">Delete</span>
            <h3>メッセージ削除</h3>
        </div>
        <div className="wf wf-pad box">
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li><code>DELETE /webhooks/&#123;id&#125;/messages/&#123;messageID&#125;</code> を使用</li>
                <li>スレッド内メッセージは <code>?thread_id=&#123;threadID&#125;</code> パラメータを付与</li>
                <li>204 No Content → 削除成功</li>
                <li>404 Not Found → すでに削除済み（成功扱い）</li>
                <li>失敗してもプロセス全体は継続（ログ出力のみ）</li>
            </ul>
        </div>

        <div className="variant-label">
            <span className="v">Rate Limiting</span>
            <h3>レート制限対応</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>HTTPステータス</th><th>対応</th></tr></thead>
                <tbody>
                    <tr><td>200 / 204 OK</td><td>成功。次のタスクへ進む</td></tr>
                    <tr><td>429 Too Many Requests</td><td><code>Retry-After</code> ヘッダの秒数待機後リトライ（最大3回）</td></tr>
                    <tr><td>500 / 502 / 503 Server Error</td><td>指数バックオフ（1s→2s→4s）でリトライ（最大3回）</td></tr>
                    <tr><td>400 / 401 / 403 / 404 Client Error</td><td>リトライなし・即失敗。AuditLog にエラー記録</td></tr>
                </tbody>
            </table>
        </div>
    </section>
);
