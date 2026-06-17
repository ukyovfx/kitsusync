window.ERDiagram = () => (
    <section className="doc" id="er-diagram">
        <div className="kicker">Database</div>
        <h1 className="docnum"><span className="n">02</span>DB Schema (ER Diagram)</h1>
        <h2 className="docsub">SQLite tables and stored operational state</h2>
        <p className="doc-intro">
            These tables are managed through GORM and stored in <code>sqlite.db</code>, where routing, setup, and audit-related application state is persisted.
            Secrets should stay out of the database unless a routing feature explicitly requires a stored value.
        </p>

        <div style={{display:'flex',flexWrap:'wrap',gap:'20px',marginBottom:'32px'}}>
            <div className="er-table">
                <div className="h"><span>tasks</span><span>Core</span></div>
                <div className="row pk"><span>id</span><span>uint PK</span></div>
                <div className="row"><span>task_id</span><span>string idx</span></div>
                <div className="row"><span>task_updated_at</span><span>string</span></div>
                <div className="row"><span>task_status</span><span>string idx</span></div>
                <div className="row"><span>comment_id</span><span>string</span></div>
                <div className="row"><span>comment_updated_at</span><span>string</span></div>
                <div className="row"><span>discord_message_id</span><span>string</span></div>
                <div className="row"><span>webhook_url</span><span>legacy / optional</span></div>
                <div className="row"><span>discord_thread_id</span><span>string</span></div>
            </div>

            <div className="er-table">
                <div className="h"><span>projects</span><span>Setup</span></div>
                <div className="row pk"><span>id</span><span>uint PK</span></div>
                <div className="row"><span>kitsu_project_id</span><span>string unique</span></div>
                <div className="row"><span>name</span><span>string</span></div>
                <div className="row"><span>project_type</span><span>cg/jissha/anime</span></div>
                <div className="row"><span>discord_category_id</span><span>string</span></div>
                <div className="row"><span>language</span><span>ja / en</span></div>
                <div className="row"><span>storage_url</span><span>string</span></div>
            </div>

            <div className="er-table">
                <div className="h"><span>project_webhooks</span><span>Routing</span></div>
                <div className="row pk"><span>id</span><span>uint PK</span></div>
                <div className="row fk"><span>kitsu_project_id</span><span>idx ? projects</span></div>
                <div className="row"><span>channel_name</span><span>string</span></div>
                <div className="row"><span>task_type</span><span>"*" = catch-all</span></div>
                <div className="row"><span>webhook_url</span><span>runtime routing</span></div>
                <div className="row"><span>discord_channel_id</span><span>string</span></div>
            </div>

            <div className="er-table">
                <div className="h"><span>audit_logs</span><span>Observability</span></div>
                <div className="row pk"><span>id</span><span>uint PK</span></div>
                <div className="row"><span>created_at</span><span>idx</span></div>
                <div className="row"><span>task_id</span><span>idx</span></div>
                <div className="row"><span>project_name</span><span>string</span></div>
                <div className="row"><span>entity_name</span><span>string</span></div>
                <div className="row"><span>old_status / new_status</span><span>string</span></div>
                <div className="row"><span>discord_msg_id</span><span>string</span></div>
                <div className="row"><span>webhook_url</span><span>masked</span></div>
                <div className="row"><span>success / retry_count</span><span>bool / int</span></div>
            </div>
        </div>

        <div className="variant-label">
            <span className="v">State</span>
            <h3>Where operational values live</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>Value</th><th>Storage</th><th>Notes</th></tr></thead>
                <tbody>
                    <tr><td>Kitsu hostname / email</td><td>settings DB</td><td>Seeded from admin UI or config seed.</td></tr>
                    <tr><td>Kitsu password</td><td>env only</td><td>Secret is kept out of the DB and loaded from runtime env.</td></tr>
                    <tr><td>Discord bot token</td><td>env only</td><td>Loaded at runtime and intentionally hidden in admin surfaces.</td></tr>
                    <tr><td>Main webhook URL</td><td>env only</td><td>Fallback secret stays outside DB state.</td></tr>
                    <tr><td>Project webhook URL</td><td>DB</td><td>Per-project routing stores webhook URLs, so docs should always mask values.</td></tr>
                    <tr><td>Task legacy webhook URL</td><td>DB/legacy</td><td>Kept for older message-linking flows and legacy compatibility.</td></tr>
                </tbody>
            </table>
        </div>
    </section>
);