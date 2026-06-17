const DBSchema = () => {
    return (
        <section className="doc" id="db-schema">
            <div className="kicker">Database</div>
            <h1 className="docnum"><span className="n">14</span>Operational Schema Notes</h1>
            <h2 className="docsub">Phase 2-2 operational tables and state notes</h2>

            <div style={{display: 'flex', gap: '24px', flexWrap: 'wrap'}}>
                <div className="er-table">
                    <div className="h"><span>project_dcc_tools</span><span>Active</span></div>
                    <div className="row pk"><span>id</span><span>uint</span></div>
                    <div className="row fk"><span>kitsu_project_id</span><span>string</span></div>
                    <div className="row"><span>dcc_type</span><span>maya / nuke ...</span></div>
                    <div className="row"><span>file_path_pattern</span><span>string</span></div>
                    <div className="row"><span>entity_type_filter</span><span>shot / asset / ""</span></div>
                    <div className="row"><span>task_type_filter</span><span>optional</span></div>
                    <div className="row"><span>enabled</span><span>bool</span></div>
                </div>

                <div className="er-table">
                    <div className="h"><span>file_path_presets</span><span>Active</span></div>
                    <div className="row pk"><span>id</span><span>uint</span></div>
                    <div className="row"><span>name</span><span>string</span></div>
                    <div className="row"><span>description</span><span>string</span></div>
                    <div className="row"><span>pattern</span><span>string</span></div>
                    <div className="row"><span>dcc_type</span><span>string</span></div>
                    <div className="row"><span>entity_type</span><span>shot / asset / ""</span></div>
                </div>

                <div className="er-table">
                    <div className="h"><span>settings</span><span>Live Config</span></div>
                    <div className="row pk"><span>key</span><span>string PK</span></div>
                    <div className="row"><span>value</span><span>string</span></div>
                    <div className="row"><span>example</span><span>kitsu.hostname</span></div>
                    <div className="row"><span>example</span><span>kitsu.email</span></div>
                    <div className="row"><span>example</span><span>discord.guildID</span></div>
                </div>
            </div>
            
            <div className="wf wf-pad box" style={{marginTop: '32px'}}>
                <div className="box-title">Schema Notes</div>
                <ul style={{margin: 0, paddingLeft: '20px', fontSize:'12px', lineHeight:'1.9'}}>
                    <li><code>project_dcc_tools</code> stores per-project DCC integration rules.</li>
                    <li><code>file_path_presets</code> is UI-editable and can also be seeded for defaults.</li>
                    <li><code>settings</code> holds live config values, while secrets should remain outside DB storage whenever possible.</li>
                    <li><code>audit_logs</code> should keep sensitive URLs masked in stored records.</li>
                </ul>
            </div>
        </section>
    );
};