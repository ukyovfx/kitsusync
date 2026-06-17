window.Routing = () => (
    <section className="doc" id="routing">
        <div className="kicker">Message Routing</div>
        <h1 className="docnum"><span className="n">05</span>Message Routing</h1>
        <h2 className="docsub">How Kitsu updates are routed to Discord destinations</h2>
        <p className="doc-intro">
            KitsuSync resolves <code>ProjectWebhook</code> routes and posts notifications to Discord channels using embed messages with status-aware color mapping.
        </p>

        <div className="variant-label">
            <span className="v">Routing Logic</span>
            <h3>Match Order</h3>
        </div>
        <div className="wf wf-pad box">
            <ol style={{margin:0,paddingLeft:'20px',fontSize:'12px',lineHeight:'1.9'}}>
                <li><strong>Exact match</strong>: Match both <code>task_type</code> and <code>entity_type</code> when available.</li>
                <li><strong>Entity fallback</strong>: If entity-type mapping exists, use the closest entity-scoped route.</li>
                <li><strong>Catch-all fallback</strong>: Use <code>task_type == "*"</code> when no specific task route exists.</li>
                <li><strong>Final fallback</strong>: Use default project routing when no mapping is found.</li>
            </ol>
        </div>

        <div className="variant-label">
            <span className="v">Entity Types</span>
            <h3>Kitsu Entity Categories</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead>
                    <tr><th>Entity Type</th><th>Example</th><th>Routing Hint</th></tr>
                </thead>
                <tbody>
                    <tr>
                        <td><span className="pill">Shot</span></td>
                        <td>SH0010, SH0020...</td>
                        <td>Most pipeline tasks are shot-scoped.</td>
                    </tr>
                    <tr>
                        <td><span className="pill">Asset</span></td>
                        <td>CHR_Hero, BG_Forest...</td>
                        <td>Asset tasks often use dedicated channels.</td>
                    </tr>
                    <tr>
                        <td><span className="pill">Sequence</span></td>
                        <td>SQ010, SQ020...</td>
                        <td>Used for sequence-level coordination updates.</td>
                    </tr>
                    <tr>
                        <td><span className="pill">Episode</span></td>
                        <td>EP01, EP02...</td>
                        <td>Common in TV-style project grouping.</td>
                    </tr>
                </tbody>
            </table>
            <div className="note" style={{position:'relative',transform:'none',marginTop:'12px',maxWidth:'none'}}>
                Discord Embed cards also map visual color by task status.
            </div>
        </div>

        <div className="variant-label">
            <span className="v">Task Types</span>
            <h3>Typical Production Mapping</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead>
                    <tr><th>Task Type</th><th>Tag</th><th>Typical Target</th></tr>
                </thead>
                <tbody>
                    <tr><td>Animation</td><td>anim</td><td>Animation channel</td></tr>
                    <tr><td>FX</td><td>fx</td><td>Effects channel</td></tr>
                    <tr><td>Lighting</td><td>lgt</td><td>Lighting channel</td></tr>
                    <tr><td>Rendering</td><td>rend</td><td>Render channel</td></tr>
                    <tr><td>Compositing</td><td>comp</td><td>Compositing channel</td></tr>
                    <tr><td>Color Grading</td><td>grade</td><td>Color and finishing channel</td></tr>
                    <tr><td>Modeling</td><td>mdl</td><td>Modeling channel</td></tr>
                    <tr><td>Texturing</td><td>tex</td><td>Texture channel</td></tr>
                    <tr><td>Shading</td><td>shd</td><td>Shading/look channel</td></tr>
                    <tr><td>Rigging</td><td>rig</td><td>Rigging channel</td></tr>
                    <tr><td>Lookdev</td><td>look</td><td>Lookdev channel</td></tr>
                    <tr><td>Storyboard</td><td>sb</td><td>Storyboarding channel</td></tr>
                    <tr><td>Background / Env</td><td>bg</td><td>Background environment channel</td></tr>
                    <tr><td>Layout</td><td>layout</td><td>Layout channel</td></tr>
                    <tr><td>Editing / Edit</td><td>edit</td><td>Edit channel</td></tr>
                    <tr><td>Sound / Audio</td><td>audio</td><td>Sound channel</td></tr>
                    <tr><td>Concept</td><td>concept</td><td>Concept design channel</td></tr>
                    <tr><td>Script</td><td>script</td><td>Scriptwriting channel</td></tr>
                    <tr><td>Design</td><td>design</td><td>Design channel</td></tr>
                    <tr><td>Default</td><td>*</td><td>Fallback when no task-specific route exists</td></tr>
                </tbody>
            </table>
        </div>
    </section>
);