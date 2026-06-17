window.StatusMap = () => (
    <section className="doc" id="status-map">
        <div className="kicker">Status System</div>
        <h1 className="docnum"><span className="n">04</span>Task Status Map</h1>
        <h2 className="docsub">Kitsu ステータスと Discord 通知の対応関係</h2>
        <p className="doc-intro">
            Kitsu のタスクステータスは Hex カラーコードを持ち、Discord Embed の左ボーダー色に自動マッピングされます。
        </p>

        <div className="variant-label">
            <span className="v">Core Statuses</span>
            <h3>主要ステータスと Discord 通知</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead>
                    <tr><th>ステータス</th><th>色</th><th>意味</th><th>通知トリガー</th></tr>
                </thead>
                <tbody>
                    <tr>
                        <td><span className="status s-todo">todo</span></td>
                        <td><code>#9a9a9a</code></td>
                        <td>未着手・アサイン済み</td>
                        <td><code>notifyOnAssign: true</code> の場合のみ通知</td>
                    </tr>
                    <tr>
                        <td><span className="status s-wip">wip</span></td>
                        <td><code>#5b7bd1</code></td>
                        <td>作業中</td>
                        <td>デフォルトではスキップ（設定次第）</td>
                    </tr>
                    <tr>
                        <td><span className="status s-wfa">wfa</span></td>
                        <td><code>#c8a13a</code></td>
                        <td>チェック待ち (Waiting for Approval)</td>
                        <td>チェッカーへメンション送信</td>
                    </tr>
                    <tr>
                        <td><span className="status s-retake">retake</span></td>
                        <td><code>#c44a2c</code></td>
                        <td>差し戻し</td>
                        <td>アーティストへメンション送信</td>
                    </tr>
                    <tr>
                        <td><span className="status s-done">done</span></td>
                        <td><code>#3e8f5b</code></td>
                        <td>承認完了</td>
                        <td>アーティストへメンション送信</td>
                    </tr>
                    <tr>
                        <td><span className="status s-ready">ready</span></td>
                        <td><code>#7a5fb0</code></td>
                        <td>次工程へ準備完了</td>
                        <td>設定による</td>
                    </tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">Status Transitions</span>
            <h3>ステータス遷移メッセージ</h3>
            <span className="desc">自動生成されるコンテキストメッセージ</span>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead>
                    <tr><th>遷移パターン</th><th>自動生成メッセージ（ja）</th></tr>
                </thead>
                <tbody>
                    <tr><td><code>TODO→WIP</code></td><td>🚀 作業を開始しました</td></tr>
                    <tr><td><code>WIP→WFA</code></td><td>📤 チェック依頼が送られました</td></tr>
                    <tr><td><code>WFA→RETAKE</code></td><td>🔄 リテイクお願いします。</td></tr>
                    <tr><td><code>WFA→WIP</code></td><td>🔧 修正作業に戻りました</td></tr>
                    <tr><td><code>WFA→DONE</code></td><td>🎉 最終承認されました！お疲れ様でした</td></tr>
                    <tr><td><code>RETAKE→WIP</code></td><td>🔧 修正作業を開始しました</td></tr>
                    <tr><td><code>WIP→RETAKE</code></td><td>🔄 リテイクお願いします。</td></tr>
                    <tr><td><code>*→DONE</code></td><td>🎉 作業完了・承認されました</td></tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">Color Rules</span>
            <h3>Discord Embed カラーロジック</h3>
        </div>
        <div className="wf wf-pad box">
            <ol style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li>Kitsu のステータスカラー（Hexコード）をそのまま Discord Embed の左ボーダー色として適用します。</li>
                <li>これにより、Kitsu上の見た目とDiscord上の見た目が完全に同期されます。</li>
                <li>無効な色の場合は、デフォルトのグレーが使用されます。</li>
            </ol>
        </div>
    </section>
);
