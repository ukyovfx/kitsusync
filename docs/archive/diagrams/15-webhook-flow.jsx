const WebhookFlow = () => {
    return (
        <section className="doc" id="webhook-flow">
            <div className="kicker">Notification Processing</div>
            <h1 className="docnum"><span className="n">15</span>Delivery Flow</h1>
            <h2 className="docsub">How webhook delivery moves from polling to diff and final message delivery</h2>

            <div className="wf wf-pad box">
                <div style={{display: 'flex', flexDirection: 'column', gap: '16px', maxWidth: '700px'}}>
                    {[
                        ['1', 'Kitsu Polling', 'Reads Tasks, Comments, Persons, Projects, and TaskTypes on each cycle.'],
                        ['2', 'State Compare', 'Compares the latest payload with sqlite.db state to decide what changed.'],
                        ['3', 'Route Resolve', 'Uses project_webhooks first, with conf.toml fallback only when routing is still configured there.'],
                        ['4', 'Discord Payload Build', 'Builds the embed body, links IDs, and adds DCC-related metadata when available.'],
                        ['5', 'Send / Update', 'Posts by webhook and updates message IDs when the same task is delivered again.'],
                        ['6', 'Audit & Persist', 'Stores task and Discord message/thread state for later updates and audit review.'],
                    ].map(([step, title, body], idx) => (
                        <div key={idx} style={{display:'flex',gap:'12px'}}>
                            <div style={{width:'24px',height:'24px',borderRadius:'50%',background: idx < 3 ? 'var(--ink)' : 'var(--accent)', color:'var(--paper)',display:'flex',alignItems:'center',justifyContent:'center',fontFamily:'var(--mono)',fontSize:'12px'}}>{step}</div>
                            <div className="box fill" style={{flex:1,borderColor: idx < 3 ? 'var(--line)' : 'var(--accent)'}}>
                                <div className="box-title" style={{color: idx < 3 ? 'var(--ink-3)' : 'var(--accent)'}}>{title}</div>
                                <p style={{margin:0,fontSize:'12px'}}>{body}</p>
                            </div>
                        </div>
                    ))}
                </div>
            </div>

            <div className="variant-label">
                <span className="v">Reality Check</span>
                <h3>Current Behavior Limits</h3>
            </div>
            <div className="wf box">
                <table className="adm">
                    <thead><tr><th>Topic</th><th>Status</th><th>Notes</th></tr></thead>
                    <tbody>
                        <tr><td>Incoming webhook</td><td>Polling</td><td><code>POST /bot/webhook</code> is not the primary production trigger path.</td></tr>
                        <tr><td>Message updates</td><td>Supported</td><td>Message ID / Thread ID are stored for follow-up updates.</td></tr>
                        <tr><td>429 / 5xx retry</td><td>Limited handling</td><td>Docs should describe manual recovery expectations clearly.</td></tr>
                    </tbody>
                </table>
            </div>
        </section>
    );
};