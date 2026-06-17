window.PhaseRoadmap = () => (
    <section className="doc" id="phase-roadmap">
        <div className="kicker">Project Evolution</div>
        <h1 className="docnum"><span className="n">19</span>Phase Roadmap</h1>
        <h2 className="docsub">How the project expands from notifications to operational tooling</h2>
        <p className="doc-intro">
            This roadmap summarizes the intended growth of KitsuSync from a focused notification tool into a more reliable operational support surface.
        </p>

        <div className="variant-label">
            <span className="v">Phase 1</span>
            <h3>Phase 1: Basic Notification System (done)</h3>
        </div>
        <div className="wf wf-pad box">
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9',color:'var(--ink-2)'}}>
                <li>Kitsu polling and task change detection</li>
                <li>Route notifications into Discord destinations</li>
                <li>Track task state with message/thread linkage</li>
            </ul>
        </div>

        <div className="variant-label">
            <span className="v">Phase 2-1</span>
            <h3>Phase 2-1: Setup & Admin Base (done)</h3>
        </div>
        <div className="wf wf-pad box">
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9',color:'var(--ink-2)'}}>
                <li><code>/setup</code> onboarding surface</li>
                <li>SQLite + GORM operational state storage</li>
                <li>Kitsu JWT-based API access foundation</li>
            </ul>
        </div>

        <div className="variant-label">
            <span className="v">Phase 2-2</span>
            <h3>Phase 2-2: Reliability & Pipeline UX (active / partial)</h3>
        </div>
        <div className="wf wf-pad box">
            <ul style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9',color:'var(--ink-2)'}}>
                <li><strong>Active:</strong> audit logs, DCC tool support, project routing, and documentation cleanup</li>
                <li><strong>In Progress:</strong> secret-handling cleanup and healthcheck improvements</li>
                <li><strong>Needs Verification:</strong> retry behavior, mention-rule UI, and path-mapping UI</li>
            </ul>
        </div>

        <div className="variant-label">
            <span className="v">Next</span>
            <h3>Next Priorities</h3>
        </div>
        <div className="wf wf-pad box">
            <ol style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9',color:'var(--ink-2)'}}>
                <li><strong>Build Recovery:</strong> keep the docs and runtime tree free from encoding and build-break regressions</li>
                <li><strong>Secret Rotation:</strong> support safer Discord token, webhook, and Kitsu password rotation workflows</li>
                <li><strong>Config Unification:</strong> clarify the boundary between DB-backed state and live config</li>
                <li><strong>Operational Hardening:</strong> tighten HTTPS assumptions and session cookie handling</li>
            </ol>
        </div>
    </section>
);