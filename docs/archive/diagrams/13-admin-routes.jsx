window.AdminRoutes = () => {
    return (
        <section className="doc" id="admin-routes">
            <div className="kicker">Admin UI</div>
            <h1 className="docnum"><span className="n">13</span>Admin Routes Map</h1>
            <h2 className="docsub">Operational route map for admin and setup surfaces</h2>
            
            <div className="wf wf-pad box">
                <div className="box-title">Routing Tree</div>
                <code>
<pre style={{margin:0, fontFamily: 'var(--mono)', fontSize: '12px', lineHeight: '1.5'}}>
/bot
├─ /login        [Kitsu login]
├─ /setup        [Setup wizard and onboarding]
├─ /admin        [Admin dashboard]
│   ├─ /admin/users           [User mapping]
│   ├─ /admin/checkers        [Checker mapping]
│   ├─ /admin/drive           [Google Drive links]
│   ├─ /admin/bot             [Bot settings]
│   ├─ /admin/dcc-tools       [DCC integration tools]
│   ├─ /admin/audit           [Audit records]
└─ /logout       [Session logout]
</pre>
                </code>
            </div>

            <div className="variant-label">
                <span className="v">Routes</span>
                <h3>Extended Admin Routes</h3>
            </div>
            <div className="wf box">
                <table className="adm">
                    <thead>
                        <tr>
                            <th>Route</th>
                            <th>Description</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr>
                            <td><code>/admin/dcc-tools</code></td>
                            <td>Entry point for Maya/Nuke helper integration workflows.</td>
                        </tr>
                        <tr>
                            <td><code>/admin/audit</code></td>
                            <td>Audit view for Discord delivery status and routing logs.</td>
                        </tr>
                    </tbody>
                </table>
            </div>
        </section>
    );
};