window.Pipeline = () => (
    <section className="doc" id="pipeline">
        <div className="kicker">Full Pipeline</div>
        <h1 className="docnum"><span className="n">08</span>Full Pipeline Flow</h1>
        <h2 className="docsub">Kitsu イベント発生から Discord 通知完了までの全処理ステップ</h2>

        <div className="wf wf-pad box">
            <div style={{display:'flex',flexDirection:'column',gap:'0'}}>
                {[
                    {n:'1', title:'イベント取得', color:'var(--ink)', items:[
                        'Polling: 定期的に Kitsu API /tasks を呼び出し',
                        'Webhook: Kitsu から POST /bot/webhook を受信',
                        '取得したタスク一覧を DB の tasks テーブルと照合',
                    ]},
                    {n:'2', title:'フィルタリング', color:'var(--ink)', items:[
                        'ignoreMessagesDaysOld: 古すぎるタスクをスキップ',
                        'ステータス変化がないタスクをスキップ（CommentUpdatedAt も確認）',
                        'silentUpdateDB: DB だけ更新して通知しないモード',
                    ]},
                    {n:'3', title:'データ収集', color:'var(--ink)', items:[
                        'Kitsu API: タスク詳細・エンティティ情報・タスクタイプ・プロジェクト名を取得',
                        '最新コメント（テキスト・作成者）を取得（skipComments=false の場合）',
                        'プレビュー画像 URL を取得（存在すれば Embed に埋め込み）',
                    ]},
                    {n:'4', title:'メッセージ組立', color:'var(--accent)', items:[
                        'テンプレートファイル（tpl/*.tpl）を使って Embed フィールドを生成',
                        'エンティティタイプで色を決定（Shot=青/Asset=緑/Other=ステータスカラー）',
                        'メンション文字列生成: アーティスト + チェッカー + MentionRule ロール',
                        'DCC ボタン (Action Row) の組立（Phase 2-2-1、設定がある場合のみ）',
                        'UseThreads=true の場合、スレッド名を生成',
                    ]},
                    {n:'5', title:'Webhook 送信', color:'var(--accent)', items:[
                        '対象チャンネルの Webhook URL を ProjectWebhook テーブルから検索（優先度順）',
                        '既存メッセージ ID がある場合は PATCH（更新）、なければ POST（新規）',
                        'UseThreads: スレッド ID があれば既存スレッドに追記、なければ新規スレッド作成',
                        '429 Rate Limit: Retry-After ヘッダ秒数だけ待機してリトライ（最大3回）',
                        '5xx Server Error: 指数バックオフ（1s→2s→4s）でリトライ（最大3回）',
                    ]},
                    {n:'6', title:'結果記録', color:'var(--accent)', items:[
                        '送信成功: DB の tasks に discord_message_id と webhook_url を保存',
                        '送信失敗: discord_message_id をクリア（次回送信時の重複防止）',
                        'AuditLog テーブルに成否・リトライ回数・エラー内容を記録（Phase 2-2-3）',
                    ]},
                ].map(step => (
                    <div key={step.n} style={{display:'flex',gap:'0',marginBottom:'0'}}>
                        <div style={{display:'flex',flexDirection:'column',alignItems:'center',width:'32px',flexShrink:0}}>
                            <div style={{width:'28px',height:'28px',borderRadius:'50%',background:step.color,color:'var(--paper)',display:'flex',alignItems:'center',justifyContent:'center',fontFamily:'var(--mono)',fontSize:'12px',fontWeight:'bold',flexShrink:0}}>
                                {step.n}
                            </div>
                            {step.n !== '6' && <div style={{width:'2px',flex:1,background:'var(--line-soft)',margin:'2px 0'}}></div>}
                        </div>
                        <div style={{flex:1,paddingLeft:'16px',paddingBottom:'20px'}}>
                            <div style={{fontWeight:'600',fontSize:'13px',marginBottom:'6px',color:step.color}}>{step.title}</div>
                            <ul style={{margin:0,paddingLeft:'18px',fontSize:'12px',lineHeight:'1.8'}}>
                                {step.items.map((item,i) => <li key={i}>{item}</li>)}
                            </ul>
                        </div>
                    </div>
                ))}
            </div>
        </div>

        <div className="variant-label">
            <span className="v">Concurrency</span>
            <h3>並列処理</h3>
        </div>
        <div className="wf wf-pad box">
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li><code>threads</code> 設定値の数だけ並列 goroutine でタスクを処理</li>
                <li>DB 書き込みは <code>Transaction</code> でアトミックに実行（race condition 防止）</li>
                <li>Webhook 送信レート: <code>discord.requestsPerMinute</code> で制限</li>
                <li>1リクエスト当たりの Embed 数: <code>discord.embedsPerRequests</code> で制限</li>
            </ul>
        </div>
    </section>
);
