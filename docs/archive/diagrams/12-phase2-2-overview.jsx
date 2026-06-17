window.Phase22Overview = () => (
    <section className="doc" id="phase-2-2-overview">
        <div className="kicker">Phase 2-2 Features</div>
        <h1 className="docnum"><span className="n">12</span>DCC Integration & Monitoring</h1>
        <h2 className="docsub">Phase 2-2 で追加された主要機能の技術的詳細</h2>
        <p className="doc-intro">
            Phase 2-2 では、通知の信頼性向上と、制作パイプラインとの深い連携（DCCツールへの直リンク）を実現しました。
        </p>

        <div className="variant-label">
            <span className="v">Reliability</span>
            <h3>Smart Retry & Webhook Management</h3>
        </div>
        <div className="wf wf-pad box">
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9',color:'var(--ink-2)'}}>
                <li><strong>429 Rate Limit 対応:</strong> Discord API から 429 エラーを受信した場合、<code>Retry-After</code> ヘッダに従って自動で待機し、再送を試みます。</li>
                <li><strong>指数バックオフ:</strong> ネットワークエラーや 5xx エラーの際、1秒 → 2秒 → 4秒 と待機時間を倍増させながらリトライします。</li>
                <li><strong>自動 Webhook 検知:</strong> ProjectWebhook テーブルから、タスクタイプに合致する Webhook URL を自動的に選択します。</li>
            </ul>
        </div>

        <div className="variant-label">
            <span className="v">Observability</span>
            <h3>Audit Trail (送信履歴の透明化)</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>ログ項目</th><th>内容</th></tr></thead>
                <tbody>
                    <tr><td>送信日時</td><td>Discord へリクエストした正確な時刻</td></tr>
                    <tr><td>ステータス</td><td>成功 (200/204) またはエラーコード</td></tr>
                    <tr><td>ターゲット</td><td>送信先のチャンネル名と Webhook URL (マスク済み)</td></tr>
                    <tr><td>リトライ回数</td><td>最終的に成功するまでにかかった試行回数</td></tr>
                    <tr><td>エラー内容</td><td>失敗した場合の API からのレスポンスボディ</td></tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">Efficiency</span>
            <h3>DCC Direct Link</h3>
        </div>
        <div className="wf wf-pad box">
            <p style={{margin:0,fontSize:'12px',lineHeight:'1.8'}}>
                Discord のボタンをクリックするだけで、ローカルの Maya や Nuke で対象のショットファイルを開くことができます。
                各プロジェクトの設定に基づき、ファイルパスを自動的に組み立てます。
            </p>
        </div>
    </section>
);
