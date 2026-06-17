window.StatusAll = () => (
    <section className="doc" id="status-all">
        <div className="kicker">Operations</div>
        <h1 className="docnum"><span className="n">10</span>Template System</h1>
        <h2 className="docsub">tpl/ ディレクトリによるメッセージテンプレートカスタマイズ</h2>
        <p className="doc-intro">
            通知メッセージのテキストは Go の <code>text/template</code> 形式でカスタマイズ可能です。
            <code>tpl/</code> ディレクトリ内の各 <code>.tpl</code> ファイルを編集することで、プロジェクトごとに異なるメッセージを出力できます。
        </p>

        <div className="variant-label">
            <span className="v">Variables</span>
            <h3>使用可能なテンプレート変数</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>変数名</th><th>型</th><th>説明</th></tr></thead>
                <tbody>
                    <tr><td><code>.TaskName</code></td><td>string</td><td>タスク名（Shot名やAsset名）</td></tr>
                    <tr><td><code>.EntityName</code></td><td>string</td><td>エンティティ名</td></tr>
                    <tr><td><code>.EntityType</code></td><td>string</td><td>Shot / Asset / Sequence 等</td></tr>
                    <tr><td><code>.ProjectName</code></td><td>string</td><td>Kitsu プロジェクト名</td></tr>
                    <tr><td><code>.TaskType</code></td><td>string</td><td>Animation / Modeling 等</td></tr>
                    <tr><td><code>.CurrentStatus</code></td><td>string</td><td>変更後のステータス</td></tr>
                    <tr><td><code>.OldStatus</code></td><td>string</td><td>変更前のステータス</td></tr>
                    <tr><td><code>.AuthorName</code></td><td>string</td><td>変更を行ったユーザー名</td></tr>
                    <tr><td><code>.CommentContent</code></td><td>string</td><td>最新のコメント内容（あれば）</td></tr>
                    <tr><td><code>.TaskURL</code></td><td>string</td><td>Kitsu タスクへの直接リンク</td></tr>
                    <tr><td><code>.Mentions</code></td><td>string</td><td>生成されたメンション文字列（@user...）</td></tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">Example</span>
            <h3>テンプレートの実装例 (default.tpl)</h3>
        </div>
        <div className="wf box" style={{fontFamily:'var(--mono)',fontSize:'12px',padding:'16px',background:'var(--paper-2)'}}>
            <pre style={{margin:0,lineHeight:'1.6'}}>{`{{.Mentions}}
**{{.TaskName}}** ({{.EntityType}}) のステータスが更新されました。
**{{.OldStatus}}** \u2192 **{{.CurrentStatus}}**

【コメント】
{{if .CommentContent}}{{.CommentContent}}{{else}}（コメントなし）{{end}}

[Kitsu で確認]({{.TaskURL}})`}</pre>
        </div>

        <div className="variant-label">
            <span className="v">Overrides</span>
            <h3>プロジェクト別オーバーライド</h3>
        </div>
        <div className="wf wf-pad box">
            <p style={{margin:0,fontSize:'12px',lineHeight:'1.7'}}>
                デフォルトでは <code>tpl/default.tpl</code> が使用されますが、<code>tpl/{"{PROJECT_ID}"}.tpl</code> を作成することで、プロジェクトごとにメッセージを完全に切り分けることが可能です。
            </p>
        </div>
    </section>
);
