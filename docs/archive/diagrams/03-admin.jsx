window.AdminUI = () => (
    <section className="doc" id="admin-ui">
        <div className="kicker">Admin Interface</div>
        <h1 className="docnum"><span className="n">03</span>Admin UI and Routes</h1>
        <h2 className="docsub">The UI surface under <code>/bot/admin</code> and mirrored <code>/admin</code> routes</h2>
        <p className="doc-intro">
            Admin pages are protected by <code>RequireSession</code> and provide the operational surface for setup, mapping, and maintenance tasks.
        </p>

        <div className="variant-label">
            <span className="v">Routes</span>
            <h3>Main admin entry points</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead>
                    <tr><th>Route</th><th>Role</th><th>Phase</th></tr>
                </thead>
                <tbody>
                    <tr><td><code>/login</code></td><td>Kitsu-backed login entry</td><td>v1</td></tr>
                    <tr><td><code>/logout</code></td><td>Session logout</td><td>v1</td></tr>
                    <tr><td><code>/setup</code></td><td>First-time setup surface</td><td>v1</td></tr>
                    <tr><td><code>/admin</code></td><td>Admin landing page</td><td>v1</td></tr>
                    <tr><td><code>/admin/users</code></td><td>Map Kitsu users to Discord IDs</td><td>v1</td></tr>
                    <tr><td><code>/admin/checkers</code></td><td>Manage checker mappings</td><td>v1</td></tr>
                    <tr><td><code>/admin/drive</code></td><td>Google Drive / storage URL management</td><td>v1</td></tr>
                    <tr><td><code>/admin/bot</code></td><td>Bot settings, Kitsu reconnect, Discord token state</td><td>Phase 2</td></tr>
                    <tr><td><code>/admin/dcc-tools</code></td><td>DCC tool integration surface</td><td>Phase 2-2</td></tr>
                    <tr><td><code>/admin/audit</code></td><td>Discord delivery audit view</td><td>Phase 2-2-3</td></tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">Setup Wizard</span>
            <h3>/setup onboarding flow</h3>
        </div>
        <div className="wf wf-pad box">
            <div className="box-title">Flow</div>
            <ol style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li>Authenticate against Kitsu and confirm connection details.</li>
                <li>Select a project type such as cg, vfx, jissha, or anime.</li>
                <li>Use the Discord Bot API to prepare categories, channels, and delivery targets.</li>
                <li>Store routing state in the DB and persist project webhook URLs for delivery.</li>
                <li>Start normal Kitsu polling after the setup path is complete.</li>
            </ol>
            <div className="note" style={{position:'relative',transform:'none',marginTop:'12px',maxWidth:'none'}}>
                <code>/setup</code> is the onboarding surface, while later operational work moves to the admin pages.
            </div>
        </div>
    </section>
);