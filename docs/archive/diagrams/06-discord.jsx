window.DiscordMessages = () => (
    <section className="doc" id="discord-messages">
        <div className="kicker">Discord Output</div>
        <h1 className="docnum"><span className="n">06</span>Discord Message Format</h1>
        <h2 className="docsub">送信される Discord Embed メッセージの構造</h2>
        <p className="doc-intro">
            Discord には Webhook を通じて <strong>Embed 付きメッセージ</strong>が送信されます。
            テンプレートファイル（<code>tpl/</code>）でカスタマイズ可能です。
        </p>

        <div className="variant-label">
            <span className="v">Standard</span>
            <h3>標準通知メッセージ (WFA例)</h3>
        </div>
        <div className="frame">
            <div className="discord">
                <div className="ch"><span className="hash">#</span> compositing-wfa</div>
                <div className="msg">
                    <div className="av">Bot</div>
                    <div>
                        <div className="meta">
                            <span className="name">Kitsu Bot</span>
                            <span className="ts">Today at 14:32</span>
                        </div>
                        <div>📤 チェック依頼が送られました <span className="mention">@checker_yamada</span></div>
                        <div className="embed s-wfa">
                            <div className="embed-title">✨ SH0010 — Compositing</div>
                            <div className="embed-sub">RETAKE → WFA • Project: FILM_2024</div>
                            <div className="embed-row"><span className="k">Entity</span><span>SQ010 / SH0010</span></div>
                            <div className="embed-row"><span className="k">Assignee</span><span>tanaka_taro</span></div>
                            <div className="embed-row"><span className="k">Status</span><span><span className="status s-wfa" style={{fontSize:'10px'}}>wfa</span></span></div>
                            <div className="embed-row"><span className="k">Drive</span><span style={{color:'#3498db'}}><u>Google Drive Link</u></span></div>
                            <div className="embed-row"><span className="k">Comment</span><span>グレインの調整完了しました</span></div>
                            <div className="footer">🔗 Kitsu で開く • 2024-05-11 14:32</div>
                        </div>
                        <div className="thread">
                            <span className="ic">thread</span>
                            <span>SH0010 Compositing — 3 replies</span>
                        </div>
                    </div>
                </div>
            </div>
        </div>

        <div className="variant-label">
            <span className="v">With DCC</span>
            <h3>DCC ボタン付きメッセージ (Phase 2-2-1)</h3>
        </div>
        <div className="frame">
            <div className="discord">
                <div className="ch"><span className="hash">#</span> compositing-wfa</div>
                <div className="msg">
                    <div className="av">Bot</div>
                    <div>
                        <div className="meta">
                            <span className="name">Kitsu Bot</span>
                            <span className="ts">Today at 14:33</span>
                        </div>
                        <div className="embed s-wip">
                            <div className="embed-title">🎞️ SH0020 — Animation</div>
                            <div className="embed-sub">TODO → WIP • Project: FILM_2024</div>
                            <div className="embed-row"><span className="k">Entity</span><span>SQ010 / SH0020</span></div>
                            <div className="embed-row"><span className="k">Status</span><span><span className="status s-wip" style={{fontSize:'10px'}}>wip</span></span></div>
                            <div className="embed-row"><span className="k">Drive</span><span style={{color:'#3498db'}}><u>Google Drive Link</u></span></div>
                            <div style={{marginTop:'8px',display:'flex',gap:'6px',flexWrap:'wrap'}}>
                                <span className="btn ghost" style={{fontSize:'11px',padding:'4px 10px',borderRadius:'4px'}}>🟢 Open in Maya</span>
                                <span className="btn ghost" style={{fontSize:'11px',padding:'4px 10px',borderRadius:'4px'}}>🔴 Open in Nuke</span>
                            </div>
                            <div className="footer">🔗 Kitsu で開く • 2024-05-11 14:33</div>
                        </div>
                    </div>
                </div>
            </div>
        </div>

        <div className="variant-label">
            <span className="v">Template</span>
            <h3>Embed フィールド構成</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead>
                    <tr><th>フィールド</th><th>内容</th><th>備考</th></tr>
                </thead>
                <tbody>
                    <tr><td>Title</td><td><code>ProcessEmoji + EntityName + TaskType</code></td><td>例: 🎞️ SH0010 — Animation</td></tr>
                    <tr><td>Description</td><td>ステータス遷移メッセージ</td><td>テンプレートで上書き可能</td></tr>
                    <tr><td>Color</td><td>ステータス色 (Hexコード) を適用</td><td>自動計算</td></tr>
                    <tr><td>Fields</td><td>Project / Entity / Assignees / Drive / Comment / Status</td><td>設定で個別 ON/OFF</td></tr>
                    <tr><td>Image</td><td>Kitsu プレビュー画像 URL</td><td>空の場合はスキップ</td></tr>
                    <tr><td>Footer</td><td>Kitsu URL + 送信日時</td><td>リンクとして機能</td></tr>
                    <tr><td>Action Rows</td><td>DCC 起動ボタン（Phase 2-2）</td><td>設定した DCC 分だけ表示</td></tr>
                </tbody>
            </table>
        </div>
    </section>
);
