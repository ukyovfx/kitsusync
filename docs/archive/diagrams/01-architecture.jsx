window.Architecture = () => (
    <section className="doc" id="architecture">
        <div className="kicker">System Overview</div>
        <h1 className="docnum"><span className="n">01</span>System Architecture</h1>
        <h2 className="docsub">How KitsuSync connects Kitsu and Discord</h2>
        <p className="doc-intro">
            KitsuSync polls Kitsu on a fixed interval, stores state in SQLite, and delivers routed Discord notifications through the Go runtime.
            The system is intentionally small so setup, operations, and troubleshooting remain manageable for small teams.
        </p>

        <div className="variant-label">
            <span className="v">Runtime</span>
            <h3>Runtime Flow</h3>
        </div>
        <div className="wf wf-pad box">
            <div style={{display:'grid',gridTemplateColumns:'1.1fr 80px 1.1fr 80px 1.2fr',gap:'12px',alignItems:'stretch'}}>
                <div className="box fill">
                    <div className="box-title">Kitsu API</div>
                    <p style={{margin:0,fontSize:'12px',lineHeight:'1.7'}}>Reads task, comment, person, project, and task type data from Kitsu.</p>
                    <div style={{marginTop:'10px'}}>
                        <span className="pill">JWT Auth</span>
                        <span className="pill">Read API</span>
                    </div>
                </div>
                <div style={{display:'flex',alignItems:'center'}}><div className="arrow-h"></div></div>
                <div className="box fill">
                    <div className="box-title">Go Poller</div>
                    <p style={{margin:0,fontSize:'12px',lineHeight:'1.7'}}>Polls on a fixed interval, computes diffs, and exposes setup and admin UI routes.</p>
                    <div style={{marginTop:'10px'}}>
                        <span className="pill solid">polling</span>
                        <span className="pill">diff</span>
                    </div>
                </div>
                <div style={{display:'flex',alignItems:'center'}}><div className="arrow-h"></div></div>
                <div className="box fill">
                    <div className="box-title">Discord Delivery</div>
                    <p style={{margin:0,fontSize:'12px',lineHeight:'1.7'}}>Delivers notifications by webhook and embed into the configured Discord destinations.</p>
                    <div style={{marginTop:'10px'}}>
                        <span className="pill">Webhook</span>
                        <span className="pill">Thread</span>
                    </div>
                </div>
            </div>
            <div style={{display:'grid',gridTemplateColumns:'1fr 1fr 1fr',gap:'12px',marginTop:'16px'}}>
                <div className="box soft">
                    <div className="box-title">State Store</div>
                    <p style={{margin:0,fontSize:'12px'}}>SQLite stores mappings, settings, and the last known notification state.</p>
                </div>
                <div className="box soft">
                    <div className="box-title">Admin Surface</div>
                    <p style={{margin:0,fontSize:'12px'}}><code>/setup</code> and <code>/admin/*</code> provide setup and operational controls.</p>
                </div>
                <div className="box soft">
                    <div className="box-title">Docs Surface</div>
                    <p style={{margin:0,fontSize:'12px'}}><code>/docs</code> serves static React-based documentation pages.</p>
                </div>
            </div>
        </div>

        <div className="variant-label">
            <span className="v">Stack</span>
            <h3>Implementation Stack</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>Layer</th><th>Tech</th><th>Role</th></tr></thead>
                <tbody>
                    <tr><td>Backend</td><td>Go</td><td>Runs polling, Discord delivery, setup UI, and HTTP endpoints.</td></tr>
                    <tr><td>Storage</td><td>SQLite + GORM</td><td>Stores project mappings, settings, and notification state.</td></tr>
                    <tr><td>Auth</td><td>Kitsu JWT + in-memory session store</td><td>Handles Kitsu API access and lightweight admin session state.</td></tr>
                    <tr><td>Delivery</td><td>Discord Webhook / Bot API</td><td>Sends routed notifications and manages setup-time Discord operations.</td></tr>
                    <tr><td>Runtime</td><td>Docker Compose + nginx</td><td>Runs the app on port 8090 and fronts it with nginx on port 80.</td></tr>
                    <tr><td>Docs</td><td>React + Babel (static)</td><td>Builds the static documentation entry from <code>site.jsx</code>.</td></tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">Security</span>
            <h3>Operational Boundaries</h3>
        </div>
        <div className="wf wf-pad box">
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9',color:'var(--ink-2)'}}>
                <li><strong>Secrets:</strong> Discord bot tokens, Kitsu passwords, and main webhook values must stay out of public docs and screenshots.</li>
                <li><strong>Session:</strong> Admin access depends on the local session flow and should be treated as an operational boundary.</li>
                <li><strong>Transport:</strong> The current UI can run on HTTP, but production exposure should assume HTTPS termination in front of the app.</li>
                <li><strong>Audit:</strong> Logs and screenshots should avoid exposing webhook URLs, tokens, or other sensitive routing details.</li>
            </ul>
        </div>
    </section>
);