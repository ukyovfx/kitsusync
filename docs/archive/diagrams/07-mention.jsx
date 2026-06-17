window.MentionSystem = () => (
    <section className="doc" id="mention-system">
        <div className="kicker">Mention System</div>
        <h1 className="docnum"><span className="n">07</span>Mention System</h1>
        <h2 className="docsub">誰に・いつ・どのステータスで通知するかを制御する3層メンション</h2>
        <p className="doc-intro">
            Kitsu-Discord Pipeline のメンションシステムは3層構造です。
            設定ファイル（<code>conf.toml</code>）による静的設定と、DB テーブル（<code>mention_rules</code>）による動的ルールを組み合わせて柔軟な通知が可能です。
        </p>

        <div className="variant-label">
            <span className="v">Layer 1</span>
            <h3>アーティストメンション</h3>
        </div>
        <div className="wf wf-pad box">
            <div className="box-title">動作条件</div>
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li><code>mention.artistStatuses</code> に含まれるステータス変化時に送信</li>
                <li>Kitsu のタスクアサイン者（Assignee）を <code>mention.userMap</code> で Discord ID に変換</li>
                <li>複数アサイン者がいる場合は全員にメンション</li>
                <li>UserMap にない場合はメンション省略（名前のみ表示）</li>
            </ul>
        </div>

        <div className="variant-label">
            <span className="v">Layer 2</span>
            <h3>チェッカーメンション</h3>
        </div>
        <div className="wf wf-pad box">
            <div className="box-title">動作条件</div>
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li><code>mention.checkerStatuses</code> に含まれるステータス変化時に送信（主に WFA）</li>
                <li><code>mention.checkers</code> でタスクタイプ別チェッカーの Discord ID を設定</li>
                <li>例: <code>taskType="Compositing"</code> → <code>discordID="&lt;@123456789&gt;"</code></li>
                <li>タスクタイプに対応するチェッカーが設定されていない場合はスキップ</li>
            </ul>
        </div>

        <div className="variant-label">
            <span className="v">Layer 3 — New</span>
            <h3>MentionRule ロールメンション (Phase 2-2-4)</h3>
        </div>
        <div className="wf wf-pad box">
            <div className="box-title">動作条件</div>
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li>DB の <code>mention_rules</code> テーブルを参照</li>
                <li>プロジェクト ID + タスクタイプ + ステータスの組み合わせでルールを検索</li>
                <li><code>task_type = ""</code> または <code>status = ""</code> のルールはワイルドカード（全タイプ/全ステータスに適用）</li>
                <li>一致するルールの Discord ロール ID すべてにメンション</li>
                <li>管理画面 <code>/admin/mention-rules</code> からルールの追加・削除が可能</li>
            </ul>
        </div>

        <div className="variant-label">
            <span className="v">Special</span>
            <h3>@here — 緊急メンション</h3>
        </div>
        <div className="wf wf-pad box">
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li><code>mention.hereStatuses</code> に含まれるステータスの場合、チャンネル内の全員に <span className="discord" style={{display:'inline'}}><span className="here">@here</span></span> でメンション</li>
                <li>主に「BLOCKED」など緊急性の高いステータスに使用</li>
                <li>過多使用を防ぐため、慎重に設定すること</li>
            </ul>
        </div>

        <div className="variant-label">
            <span className="v">Config</span>
            <h3>conf.toml 設定例</h3>
        </div>
        <div className="wf box" style={{fontFamily:'var(--mono)',fontSize:'12px'}}>
            <pre style={{margin:0,lineHeight:'1.7'}}>{`[mention]
artistStatuses  = ["RETAKE", "DONE"]
checkerStatuses = ["WFA"]
hereStatuses    = ["BLOCKED"]

[[mention.userMap]]
kitsuName  = "tanaka_taro"
discordID  = "123456789012345678"

[[mention.checkers]]
taskType  = "Compositing"
discordID = "987654321098765432"

[notification]
notifyOnAssign = true  # TODO ステータス（新規アサイン）も通知する`}</pre>
        </div>
    </section>
);
