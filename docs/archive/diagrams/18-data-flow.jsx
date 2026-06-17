window.DataFlow = () => (
    <section className="doc" id="dataflow">
        <div className="kicker">System Architecture</div>
        <h1 className="docnum"><span className="n">18</span>Data Flow & State Machine</h1>
        <h2 className="docsub">How Kitsu changes become Discord notifications</h2>
        <p className="doc-intro">
            This view summarizes the runtime path from Kitsu polling to Discord delivery, including routing, persistence, and state transitions.
        </p>

        <div className="variant-label">
            <span className="v">Flow</span>
            <h3>Processing Stages</h3>
        </div>
        <div className="wf wf-pad box">
            <div style={{display:'grid',gridTemplateColumns:'repeat(3,1fr)',gap:'12px'}}>
                <div className="box fill"><div className="box-title">Input</div><p style={{margin:0,fontSize:'12px'}}>Kitsu API payload enters the system, including 404 or auth failure conditions.</p></div>
                <div className="box fill"><div className="box-title">Diff</div><p style={{margin:0,fontSize:'12px'}}>DB state is compared so unchanged tasks can be filtered out.</p></div>
                <div className="box fill"><div className="box-title">Route</div><p style={{margin:0,fontSize:'12px'}}>project_webhooks resolve the destination by taskType and project mapping.</p></div>
                <div className="box fill"><div className="box-title">Render</div><p style={{margin:0,fontSize:'12px'}}>Embed payload is assembled and DCC fields are added when available.</p></div>
                <div className="box fill"><div className="box-title">Send</div><p style={{margin:0,fontSize:'12px'}}>Discord Webhook delivery records Message ID / Thread ID for later updates.</p></div>
                <div className="box fill"><div className="box-title">Persist</div><p style={{margin:0,fontSize:'12px'}}>tasks / audit_logs store delivery state and audit evidence.</p></div>
            </div>
        </div>

        <div className="variant-label">
            <span className="v">State Machine</span>
            <h3>Typical Status Transitions</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>Transition</th><th>Expected Discord Behavior</th></tr></thead>
                <tbody>
                    <tr><td>TODO ? WIP</td><td>Post or update the task card with the new active status.</td></tr>
                    <tr><td>WIP ? WFA</td><td>Move attention toward review-ready state and apply mention rules if configured.</td></tr>
                    <tr><td>WFA ? RETAKE</td><td>Surface the returned status and keep the relevant comment context visible.</td></tr>
                    <tr><td>WFA ? DONE</td><td>Mark the task as completed and preserve the final thread/message trail.</td></tr>
                </tbody>
            </table>
        </div>

        <div className="variant-label">
            <span className="v">Cleanup</span>
            <h3>Data Hygiene</h3>
        </div>
        <div className="wf wf-pad box">
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.8'}}>
                <li>Keep DB records trimmed so secret-like values are not retained beyond what the runtime needs.</li>
                <li><code>tasks.webhook_url</code> remains a legacy field and should not be expanded without a clear migration plan.</li>
                <li><code>audit_logs</code> should keep URL-like values masked in persisted output.</li>
            </ul>
        </div>
    </section>
);