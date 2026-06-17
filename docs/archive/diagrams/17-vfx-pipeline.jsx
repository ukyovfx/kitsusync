window.VFXPipeline = () => (
    <section className="doc" id="vfx-pipeline">
        <div className="kicker">Pipeline & Workflows</div>
        <h1 className="docnum"><span className="n">17</span>Pipeline Flow & Task Types</h1>
        <h2 className="docsub">Kitsuのエンティティ構造、タスクカテゴリ、およびユーザーワークフロー</h2>
        <p className="doc-intro">
            システムが対応しているプロジェクト・エンティティ・タスク型のマッピング情報と、Kitsu-Discord連携における実務のワークフローを定義します。
        </p>

        {/* --- Entity Types --- */}
        <div className="variant-label">
            <span className="v">Classification</span>
            <h3>🗂️ Entity Type Hierarchy</h3>
        </div>
        <div className="wf box">
            <svg viewBox="0 0 480 300" xmlns="http://www.w3.org/2000/svg" style={{background: 'var(--paper-2)', border: '1px solid var(--line)', borderRadius: '4px'}}>
                <rect x="180" y="20" width="120" height="50" fill="none" stroke="var(--ink)" strokeWidth="2" rx="4"/>
                <text x="240" y="48" fontSize="13" fontWeight="600" textAnchor="middle" fill="var(--ink)">Project</text>

                <path d="M 240 70 L 100 120" stroke="var(--ink)" strokeWidth="2"/>
                <path d="M 240 70 L 240 120" stroke="var(--ink)" strokeWidth="2"/>
                <path d="M 240 70 L 380 120" stroke="var(--ink)" strokeWidth="2"/>

                <rect x="40" y="120" width="120" height="50" fill="none" stroke="var(--ink)" strokeWidth="2" rx="4"/>
                <text x="100" y="142" fontSize="12" fontWeight="600" textAnchor="middle" fill="var(--ink)">Sequence</text>
                <text x="100" y="160" fontSize="10" textAnchor="middle" fill="var(--ink-3)">(Episode, Acta)</text>

                <rect x="180" y="120" width="120" height="50" fill="none" stroke="var(--ink)" strokeWidth="2" rx="4"/>
                <text x="240" y="148" fontSize="12" fontWeight="600" textAnchor="middle" fill="var(--ink)">Shot</text>
                <text x="240" y="166" fontSize="10" textAnchor="middle" fill="var(--ink-3)">(Camera setup)</text>

                <rect x="320" y="120" width="120" height="50" fill="none" stroke="var(--ink)" strokeWidth="2" rx="4"/>
                <text x="380" y="148" fontSize="12" fontWeight="600" textAnchor="middle" fill="var(--ink)">Asset</text>
                <text x="380" y="166" fontSize="10" textAnchor="middle" fill="var(--ink-3)">(Character, Prop)</text>

                <text x="100" y="210" fontSize="10" fontWeight="600" fill="var(--ink)">Tasks in Sequence:</text>
                <text x="100" y="225" fontSize="9" fill="var(--ink-3)">• Script, Concept, Edit</text>
                <text x="100" y="237" fontSize="9" fill="var(--ink-3)">• Color Grading, Sound</text>

                <text x="240" y="210" fontSize="10" fontWeight="600" fill="var(--ink)">Tasks in Shot:</text>
                <text x="240" y="225" fontSize="9" fill="var(--ink-3)">• Layout, Animation, FX</text>
                <text x="240" y="237" fontSize="9" fill="var(--ink-3)">• Lighting, Comp</text>

                <text x="380" y="210" fontSize="10" fontWeight="600" fill="var(--ink)">Tasks in Asset:</text>
                <text x="380" y="225" fontSize="9" fill="var(--ink-3)">• Modeling, Rigging</text>
                <text x="380" y="237" fontSize="9" fill="var(--ink-3)">• Shading, Texturing</text>
            </svg>
        </div>

        {/* --- Task Matrix --- */}
        <div style={{display:'grid', gridTemplateColumns:'1fr 1fr', gap:'20px', marginBottom:'2rem'}}>
            <div>
                <div className="variant-label">
                    <span className="v">Type: CG</span>
                    <h3>🎥 CG/VFX Task Types</h3>
                </div>
                <div className="wf box">
                    <table className="adm">
                        <thead><tr><th>Entity</th><th>Discord Channel</th></tr></thead>
                        <tbody>
                            <tr><td><strong>Sequence</strong></td><td>#concept, #edit, #color</td></tr>
                            <tr><td><strong>Asset</strong></td><td>#modeling, #rigging, #shading</td></tr>
                            <tr><td><strong>Shot</strong></td><td>#layout, #animation, #fx, #lighting, #render, #comp</td></tr>
                        </tbody>
                    </table>
                </div>
            </div>
            <div>
                <div className="variant-label">
                    <span className="v">Type: Anime</span>
                    <h3>🎨 アニメ Task Types</h3>
                </div>
                <div className="wf box">
                    <table className="adm">
                        <thead><tr><th>Entity</th><th>Discord Channel</th></tr></thead>
                        <tbody>
                            <tr><td><strong>Sequence</strong></td><td>#concept, #storyboard</td></tr>
                            <tr><td><strong>Episode</strong></td><td>#edit, #sound, #color</td></tr>
                            <tr><td><strong>Shot</strong></td><td>#animation, #inbetween, #coloring</td></tr>
                            <tr><td><strong>Asset</strong></td><td>#background, #props</td></tr>
                        </tbody>
                    </table>
                </div>
            </div>
        </div>

        {/* --- Icon Map --- */}
        <div className="variant-label">
            <span className="v">Icon Map</span>
            <h3>✨ 工程アイコン (Task Type Icons)</h3>
        </div>
        <div className="wf wf-pad box">
            <p style={{fontSize:'12px',marginBottom:'16px',color:'var(--ink-2)'}}>様々なカテゴリーに対応した作業工程（タスクタイプ）アイコンの全一覧です。</p>
            <div style={{display:'flex',gap:'8px',flexWrap:'wrap'}}>
                {[
                    {name: 'Animation', e: '🎞️'},
                    {name: 'Compositing', e: '✨'},
                    {name: 'FX', e: '💥'},
                    {name: 'Lighting', e: '💡'},
                    {name: 'Modeling', e: '🧊'},
                    {name: 'Rigging', e: '🦴'},
                    {name: 'Texturing', e: '🖌️'},
                    {name: 'Lookdev', e: '👁️'},
                    {name: 'Layout', e: '🎥'},
                    {name: 'Editing', e: '✂️'},
                    {name: 'Rendering', e: '🖼️'},
                    {name: 'Color Grading', e: '🎨'},
                    {name: 'Shading', e: '🎭'},
                    {name: 'Concept', e: '📝'},
                    {name: 'Script', e: '📜'},
                    {name: 'Design', e: '📐'},
                    {name: 'Storyboard', e: '🗒️'},
                    {name: 'Background', e: '🏞️'},
                    {name: 'Sound', e: '🔊'}
                ].map(icon => (
                    <div key={icon.name} style={{display:'flex',alignItems:'center',gap:'6px',padding:'6px 12px',background:'var(--paper-2)',border:'1px solid var(--line)',borderRadius:'4px'}}>
                        <span style={{fontSize:'16px'}}>{icon.e}</span>
                        <span style={{fontFamily:'var(--mono)',fontSize:'11px',fontWeight:'bold',color:'var(--ink-2)'}}>{icon.name}</span>
                    </div>
                ))}
            </div>
        </div>

        {/* --- Workflows --- */}
        <div className="variant-label">
            <span className="v">User Journey</span>
            <h3>👤 初期セットアップフロー (Initial Setup Workflow)</h3>
        </div>
        <div className="wf box">
            <svg viewBox="0 0 480 380" xmlns="http://www.w3.org/2000/svg" style={{background: 'var(--paper-2)', border: '1px solid var(--line)', borderRadius: '4px', padding:'1rem'}}>
                <defs>
                    <marker id="arrow-dark" markerWidth="10" markerHeight="10" refX="9" refY="3" orient="auto">
                        <polygon points="0 0, 10 3, 0 6" fill="var(--ink)"/>
                    </marker>
                </defs>
                <circle cx="240" cy="30" r="15" fill="var(--ink)"/>
                <text x="240" y="35" fontSize="10" fill="var(--paper)" textAnchor="middle" fontWeight="600">1</text>
                <text x="240" y="55" fontSize="11" fontWeight="600" textAnchor="middle" fill="var(--ink)">Admin Access</text>

                <rect x="140" y="70" width="200" height="40" fill="none" stroke="var(--ink)" strokeWidth="2" rx="4"/>
                <text x="240" y="90" fontSize="11" fontWeight="600" textAnchor="middle" fill="var(--ink)">Create Project</text>
                <text x="240" y="102" fontSize="9" textAnchor="middle" fill="var(--ink-3)">(Kitsu ID + Discord Guild)</text>
                <path d="M 240 45 L 240 70" stroke="var(--ink)" strokeWidth="2" markerEnd="url(#arrow-dark)"/>

                <path d="M 240 110 L 240 140" stroke="var(--ink)" strokeWidth="2" markerEnd="url(#arrow-dark)"/>
                <rect x="140" y="140" width="200" height="40" fill="none" stroke="var(--ink)" strokeWidth="2" rx="4"/>
                <text x="240" y="160" fontSize="11" fontWeight="600" textAnchor="middle" fill="var(--ink)">Select Template</text>
                <text x="240" y="172" fontSize="9" textAnchor="middle" fill="var(--ink-3)">(CG / 実写 / アニメ)</text>

                <path d="M 240 180 L 240 210" stroke="var(--ink)" strokeWidth="2" markerEnd="url(#arrow-dark)"/>
                <rect x="140" y="210" width="200" height="40" fill="none" stroke="var(--ink)" strokeWidth="2" rx="4"/>
                <text x="240" y="230" fontSize="11" fontWeight="600" textAnchor="middle" fill="var(--ink)">Auto-Create Channels</text>
                <text x="240" y="242" fontSize="9" textAnchor="middle" fill="var(--ink-3)">(+ internal routing)</text>

                <path d="M 240 250 L 240 280" stroke="var(--ink)" strokeWidth="2" markerEnd="url(#arrow-dark)"/>
                <rect x="140" y="280" width="200" height="40" fill="none" stroke="var(--ink)" strokeWidth="2" rx="4"/>
                <text x="240" y="300" fontSize="11" fontWeight="600" textAnchor="middle" fill="var(--ink)">Map Users (Kitsu ↔ Discord)</text>
                <text x="240" y="312" fontSize="9" textAnchor="middle" fill="var(--ink-3)">(For mentions)</text>
                
                <path d="M 240 320 L 240 340" stroke="#2e7d32" strokeWidth="2" markerEnd="url(#arrow-dark)"/>
                <circle cx="240" cy="350" r="10" fill="#2e7d32"/>
                <text x="240" y="353" fontSize="10" fill="white" textAnchor="middle" fontWeight="600">✓</text>
            </svg>
        </div>

        <div className="variant-label">
            <span className="v">Troubleshooting</span>
            <h3>🔧 エラー解決フロー (Issue Resolution)</h3>
        </div>
        <div className="wf box">
            <table className="adm">
                <thead><tr><th>Problem</th><th>Action / Fix</th></tr></thead>
                <tbody>
                    <tr><td><strong>Notification fails (Webhook error)</strong></td><td>Check the main webhook config and routing rules in /admin/bot</td></tr>
                    <tr><td><strong>Mention not working</strong></td><td>Add User Mapping (Kitsu email ↔ Discord ID) in /admin/users</td></tr>
                    <tr><td><strong>Wrong channels used</strong></td><td>Check Project Template settings and re-sync channels</td></tr>
                </tbody>
            </table>
        </div>
    </section>
);
