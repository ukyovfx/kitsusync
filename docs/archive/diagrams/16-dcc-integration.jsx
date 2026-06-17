const DCCIntegration = () => {
    return (
        <section className="doc" id="dcc-integration">
            <div className="kicker">DCC Integration</div>
            <h1 className="docnum"><span className="n">16</span>vfx-launcher Integration</h1>
            <h2 className="docsub">Discord からローカル DCC ツールへのシームレスなブリッジ</h2>

            <div className="wf wf-pad box">
                <div className="box-title">System Architecture</div>
                <table className="adm">
                    <thead>
                        <tr>
                            <th>コンポーネント</th>
                            <th>役割</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr>
                            <td><strong>Discord Action Row</strong></td>
                            <td>ユーザーに表示される DCC 起動ボタン（メッセージに埋め込み）</td>
                        </tr>
                        <tr>
                            <td><strong>URL Scheme</strong></td>
                            <td><code>vfx-launcher://open?software=X&path=Y</code> のプロトコルハンドラ</td>
                        </tr>
                        <tr>
                            <td><strong>Python Launcher</strong></td>
                            <td>ローカルOS（Windows/macOS/Linux）で URL を受け取り、適切な DCC アプリケーションの実行プロセスを起動</td>
                        </tr>
                    </tbody>
                </table>
            </div>
            
            <div className="variant-label">
                <span className="v">FLOW</span>
                <h3>実行フロー</h3>
            </div>
            <div className="wf wf-pad box">
                <ol style={{margin: 0, paddingLeft: '20px', lineHeight: '1.8'}}>
                    <li>ユーザーが Discord 上の <span className="pill solid">🔴 Open in Nuke</span> 等のボタンをクリック。</li>
                    <li>OS (ブラウザ) が <code>vfx-launcher://</code> プロトコルを検知し、ローカルにインストールされた Python スクリプトに引数を渡す。</li>
                    <li>Python スクリプトが引数のパスを解釈し、対応する DCC の実行ファイルにパスを渡してサブプロセスとして起動。</li>
                </ol>
            </div>

            <div className="variant-label">
                <span className="v">CONFIG</span>
                <h3>対応DCCと引数テンプレート</h3>
            </div>
            <div className="wf box">
                <table className="adm">
                    <thead>
                        <tr>
                            <th>DCC</th>
                            <th>コマンド引数（例）</th>
                            <th>備考</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr>
                            <td><span className="pill" style={{borderColor: '#2ecc71', color: '#2ecc71'}}>Maya</span></td>
                            <td><code>-file {'{FILE}'}</code></td>
                            <td>シーンファイルとして開く</td>
                        </tr>
                        <tr>
                            <td><span className="pill" style={{borderColor: '#e74c3c', color: '#e74c3c'}}>Nuke</span></td>
                            <td><code>{'{FILE}'}</code></td>
                            <td>コンプスクリプトとして開く</td>
                        </tr>
                        <tr>
                            <td><span className="pill" style={{borderColor: '#e67e22', color: '#e67e22'}}>Blender</span></td>
                            <td><code>{'{FILE}'}</code></td>
                            <td>.blend ファイルを開く</td>
                        </tr>
                        <tr>
                            <td><span className="pill" style={{borderColor: '#f1c40f', color: '#f39c12'}}>Houdini</span></td>
                            <td><code>{'{FILE}'}</code></td>
                            <td>.hip ファイルを開く</td>
                        </tr>
                    </tbody>
                </table>
            </div>
        </section>
    );
};
