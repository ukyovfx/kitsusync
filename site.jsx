const { useEffect, useState } = React;

const sections = {
  ja: [
    {
      id: 'overview',
      num: '01',
      label: '概要',
      kicker: 'System Overview',
      title: 'Kitsu x Discord Pipeline',
      subtitle: '現在運用中の構成と役割',
      intro: 'このアプリは Kitsu/Zou を定期ポーリングし、ステータス変化やコメント更新を検出して Discord へ通知します。公開版では DCC 連携は含めず、運用に必要なセットアップ、割り当て、監査、プレビュー連携を残しています。',
      bullets: ['Kitsu API から tasks / task statuses / entities / projects / task types / persons / comments を取得', 'SQLite 状態を使って差分を判定', 'Discord へ embed と message update を送信', 'preview_file_id があるときは画像プレビュー URL を埋め込む', '監査ログと daily summary を管理画面から追跡できる']
    },
    {
      id: 'routes',
      num: '02',
      label: 'ルート',
      kicker: 'Routing',
      title: '公開ルーティング',
      subtitle: 'nginx と bot app の現在の入口',
      table: {
        headers: ['Path', '役割'],
        rows: [
          ['/','Kitsu 本体'],
          ['/bot/login','管理画面ログイン'],
          ['/bot/setup','Project Setup / Bot 初期設定'],
          ['/bot/admin','管理画面ホーム'],
          ['/bot/admin/users','ユーザー割り当て'],
          ['/bot/admin/checkers','チェッカー割り当て'],
          ['/bot/admin/bot','Bot 設定'],
          ['/bot/admin/drive','ストレージリンク'],
          ['/bot/admin/audit','監査ログ']
        ]
      }
    },
    {
      id: 'setup',
      num: '03',
      label: 'セットアップ',
      kicker: 'Project Setup',
      title: 'Project Setup と Bot 初期設定',
      subtitle: '管理者が最初に触る導線',
      bullets: ['Bot 初期設定では公開ホストから Kitsu hostname を自動検出し、スタジオ管理者メール / パスワードだけで Bot アカウントを作成', 'Project Setup は Kitsu プロジェクトを選び、Discord カテゴリとテキストチャンネル群を作成', 'プロジェクトタイプは CG / VFX、実写、アニメをサポート', '作成したチャンネルには webhook を発行し、project_webhooks テーブルへ保存', '既存プロジェクトは setup 画面から削除とチャンネル追加 / 削除を行う']
    },
    {
      id: 'channels',
      num: '04',
      label: 'チャンネル',
      kicker: 'Discord Structure',
      title: 'チャンネル構成と通知先',
      subtitle: 'テンプレートと全体通知の扱い',
      intro: '各プロジェクトには task type ごとのチャンネルと、`*` を担当する全体通知チャンネルを持てます。daily summary はこの全体通知 webhook だけに送ります。',
      bullets: ['CG / VFX、実写、アニメの各テンプレートを保持', 'テンプレート外の Custom チャンネルも追加可能', 'task type が一致する webhook を最優先で使用し、必要に応じて `*` の全体通知 webhook やメイン webhook にフォールバック', 'daily summary は `*` の全体通知 webhook のみを使用', 'チャンネル追加 / 削除は Discord 側へ即時反映']
    },
    {
      id: 'assignments',
      num: '05',
      label: '割り当て',
      kicker: 'Assignments',
      title: 'ユーザー割り当てとチェッカー割り当て',
      subtitle: '通知先の個人解決ルール',
      bullets: ['ユーザー割り当ては Kitsu ユーザーと Discord ID の対応を保持', 'Kitsu Bot アカウントは割り当て候補から除外', 'チェッカー割り当ては task type ごとにユーザーを選択し、通常は User Assignment から Discord ID を自動解決', '編集時のみ Resolved ID を直接上書きできる', 'Discord ID が無い場合は管理画面上で IDなし と表示']
    },
    {
      id: 'notifications',
      num: '06',
      label: '通知',
      kicker: 'Notification Flow',
      title: 'Kitsu から Discord への流れ',
      subtitle: '差分検出、送信、更新',
      bullets: ['poll で取得した Kitsu データを整形し、差分がある task のみを抽出', 'embed の色は Kitsu status color を使用', '同じ task は前回の Discord message ID を使って更新を試みる', 'コメントや状態変化ごとに監査ログへ保存', 'duplicate send を防ぐため poll 実行は mutex で保護']
    },
    {
      id: 'preview',
      num: '07',
      label: 'プレビュー',
      kicker: 'Preview & Storage',
      title: '画像プレビューと補助リンク',
      subtitle: 'Discord embed に載る補助情報',
      bullets: ['プレビュー画像 URL は `{KITSU_PUBLIC_HOSTNAME or KITSU_HOSTNAME}/api/pictures/thumbnails/preview-files/{id}.png`', 'preview が無ければテキスト通知のみ送信', 'Storage Links では project ごとに Drive などの補助 URL を設定可能', 'Storage Links は webhook ではなく embed 補助リンク用の設定']
    },
    {
      id: 'audit',
      num: '08',
      label: '監査',
      kicker: 'Audit & Summary',
      title: '監査ログと Daily Summary',
      subtitle: '送信結果の見える化',
      bullets: ['監査ログは時刻、対象、task type、状態、message ID、結果を表示', 'daily summary は status 集計を embed 化して送信', 'summary の送信先は project の全体通知 webhook のみ', '通常通知では routing 状況に応じて main webhook fallback が発生し得る']
    },
    {
      id: 'data',
      num: '09',
      label: 'データ',
      kicker: 'Data Model',
      title: '主要テーブルと設定値',
      subtitle: '管理画面と通知に関わる保存先',
      table: {
        headers: ['項目', '用途'],
        rows: [
          ['projects','Kitsu project と Discord category の対応'],
          ['project_webhooks','channel 名、task type、webhook URL、Discord channel ID'],
          ['user_maps','Kitsu user と Discord ID の対応'],
          ['checker_maps','task type ごとの checker ユーザーと解決済み ID'],
          ['audit_logs','Discord 送信結果'],
          ['settings','kitsu.hostname / kitsu.email / discord.guildID など']
        ]
      }
    },
    {
      id: 'security',
      num: '10',
      label: '公開方針',
      kicker: 'Security Notes',
      title: '公開版の安全方針',
      subtitle: '残しているものと消しているもの',
      bullets: ['secrets は画面に平文表示しない', 'Webhook URL は docs へ露出しない', 'DCC launcher / dcc-tools は公開版から削除', '管理画面は Kitsu manager / admin ログインで保護', 'trusted reverse proxy 配下で HTTPS 終端し、アプリを直接インターネットへ公開しない', 'X-Forwarded-Proto は trusted reverse proxy が上書きする前提で Secure Cookie 判定に使う']
    }
  ],
  en: []
};
sections.en = [
  { ...sections.ja[0], label: 'Overview', kicker: 'System Overview', title: 'Kitsu x Discord Pipeline', subtitle: 'Current production architecture and responsibilities', intro: 'This app polls Kitsu/Zou, detects status or comment changes, and sends Discord notifications. The public build removes DCC integrations while keeping setup, assignment, audit, and preview workflows needed for operations.', bullets: ['Fetches tasks, task statuses, entities, projects, task types, persons, and comments from Kitsu APIs', 'Uses SQLite state to detect changes', 'Sends Discord embeds and message updates', 'Adds preview image URLs when preview_file_id is present', 'Exposes audit logs and daily summary through the admin UI'] },
  { ...sections.ja[1], label: 'Routes', subtitle: 'Current nginx and bot-app entry points', table: { headers: ['Path','Purpose'], rows: [['/','Kitsu application'],['/bot/login','Admin login'],['/bot/setup','Project setup and bot bootstrap'],['/bot/admin','Admin home'],['/bot/admin/users','User assignment'],['/bot/admin/checkers','Checker assignment'],['/bot/admin/bot','Bot settings'],['/bot/admin/drive','Storage links'],['/bot/admin/audit','Audit log']] } },
  { ...sections.ja[2], label: 'Setup', subtitle: 'First-run workflow for administrators', bullets: ['Bot bootstrap auto-detects the public Kitsu hostname and only asks for studio admin email and password', 'Project Setup creates Discord categories and text channels from a selected Kitsu project', 'Supports CG / VFX, Live Action, and Anime project types', 'Creates webhooks for each channel and stores them in project_webhooks', 'Existing projects can be deleted or have channels added and removed from the setup page'] },
  { ...sections.ja[3], label: 'Channels', subtitle: 'Templates and general notification routing', intro: 'Each project can have task-type channels plus a general channel mapped to `*`. Daily summary is sent only to this general webhook.', bullets: ['Keeps templates for CG / VFX, Live Action, and Anime', 'Supports custom channels in addition to templates', 'Task-specific webhooks are preferred; routing can fall back to the `*` general webhook and then the main webhook when needed', 'Daily summary uses only the `*` general webhook', 'Channel add and delete actions apply to Discord immediately'] },
  { ...sections.ja[4], label: 'Assignments', subtitle: 'How individual mentions are resolved', bullets: ['User Assignment stores Kitsu user to Discord ID mappings', 'The Kitsu bot account is excluded from assignment candidates', 'Checker Assignment stores a user per task type and normally resolves the Discord ID from User Assignment', 'Edit mode can override the Resolved ID directly', 'Missing IDs are shown as No ID in the UI'] },
  { ...sections.ja[5], label: 'Notifications', subtitle: 'Diff detection, send, and update pipeline', bullets: ['Polls Kitsu and filters only tasks with meaningful changes', 'Embed colors follow the Kitsu status color', 'Existing Discord message IDs are reused when updating the same task', 'Audit log entries are written for status or comment deliveries', 'A mutex protects the poller from duplicate send cycles'] },
  { ...sections.ja[6], label: 'Preview', subtitle: 'Image preview and helper links', bullets: ['Preview URL format is `{KITSU_PUBLIC_HOSTNAME or KITSU_HOSTNAME}/api/pictures/thumbnails/preview-files/{id}.png`', 'If no preview is available, the message is sent without an image', 'Storage Links store optional Drive or file links per project', 'Storage Links are helper URLs for embeds, not webhook settings'] },
  { ...sections.ja[7], label: 'Audit', subtitle: 'Delivery visibility and daily rollup', bullets: ['Audit Log shows time, target, task type, status, message ID, and result', 'Daily summary aggregates task statuses into one embed', 'Summary delivery uses only the project general webhook', 'Regular task routing may still use main-webhook fallback depending on mapping'] },
  { ...sections.ja[8], label: 'Data', subtitle: 'Main tables and persisted settings', table: { headers: ['Item','Purpose'], rows: [['projects','Maps Kitsu projects to Discord categories'],['project_webhooks','Stores channel name, task type, webhook URL, and Discord channel ID'],['user_maps','Stores Kitsu user to Discord ID mappings'],['checker_maps','Stores checker user per task type and resolved IDs'],['audit_logs','Stores Discord delivery results'],['settings','Stores kitsu.hostname, kitsu.email, discord.guildID, and related values']] } },
  { ...sections.ja[9], label: 'Security', subtitle: 'What stays and what is removed in the public build', bullets: ['Secrets are not shown in plain text', 'Webhook URLs are not exposed in docs', 'DCC launcher integrations and dcc-tools are removed', 'The admin UI is protected by Kitsu manager/admin login', 'Run KitsuSync behind a trusted reverse proxy with HTTPS termination; do not expose the app directly to the internet', 'X-Forwarded-Proto is trusted only when overwritten by that proxy and is used for Secure Cookie decisions'] }
];

const pipelineDocs = {
  ja: [
    {
      title: 'Kitsu Poll / Diff / Discord 通知',
      overview: '定期ポーリングで Kitsu の task / status / comment を取得し、DB上の前回状態と比較して Discord へ送信します。',
      steps: [['Kitsu API', 'tasks / statuses / comments'], ['Diff', 'SQLite marker と比較'], ['Build', 'embed / preview / mentions'], ['Webhook', 'task type または全体通知'], ['Audit', '送信結果を保存']],
      detail: ['ステータス・コメント変更通知', 'Kitsu API / DB state', 'Discord embed と message update', 'tasks / audit_logs', '前回poll中ならスキップし二重送信を防止']
    },
    {
      title: 'Bot Initial Setup',
      overview: '公開ホストを自動検出し、スタジオ管理者認証から Kitsu bot account を作成して設定へ保存します。',
      steps: [['Public Host', 'Host header から検出'], ['Admin Auth', '管理者メール / パスワード'], ['Create Bot', 'Kitsu bot account'], ['Persist', 'settings と env'], ['Reconnect', 'JWT 更新']],
      detail: ['初回接続準備', '公開URL / 管理者認証', 'kitsu.hostname / kitsu.email', 'settings', '認証失敗時はProject Setup上にエラー表示']
    },
    {
      title: 'Project Setup / Discord 作成',
      overview: 'Kitsu project を選択し、プロジェクト種別テンプレートに沿って Discord category / channel / webhook を作成します。',
      steps: [['Select Project', 'Kitsu project'], ['Template', 'CG / 実写 / アニメ'], ['Discord', 'category / channel'], ['Webhook', 'channelごとに作成'], ['DB', 'projects / project_webhooks']],
      detail: ['プロジェクト通知導線の生成', 'Kitsu project / project type / language', 'Discord実体とWebhook', 'projects / project_webhooks', '一部channel作成失敗は結果画面にWARN/FAIL表示']
    },
    {
      title: 'Channel Management',
      overview: '設定済みプロジェクトのチャンネルを展開し、テンプレートまたは custom で追加、不要なチャンネルを削除します。',
      steps: [['Open Project', 'accordion'], ['Add', 'template / custom'], ['Create', 'Discord channel'], ['Webhook', 'project webhook'], ['Delete', 'DiscordとDBから削除']],
      detail: ['運用中の通知先調整', 'project_webhooks / Discord category', 'channelとwebhookの即時反映', 'project_webhooks', '重複名やDiscord失敗は画面にエラー表示']
    },
    {
      title: 'User / Checker Assignment',
      overview: 'Kitsu user と Discord ID を紐づけ、checker は task type ごとに user を選び、通知時にIDを解決します。',
      steps: [['User', 'Kitsu person 選択'], ['Discord ID', 'user_maps'], ['Checker', 'task type'], ['Resolve', 'User Assignment参照'], ['Mention', 'Discord通知へ反映']],
      detail: ['個人メンションの解決', 'Kitsu person / Discord ID / task type', 'resolved Discord ID', 'user_maps / checker_maps', 'ID未設定時はIDなし表示で管理画面へ誘導']
    },
    {
      title: 'Preview / Storage Links',
      overview: 'preview_file_id からKitsu画像URLを組み立て、Storage Links は projectごとの補助リンクとして embed に添えます。',
      steps: [['Task', 'preview_file_id'], ['URL', 'preview-files/{id}.png'], ['Storage', 'project link'], ['Embed', '画像 / 補助URL'], ['Send', 'Discord message']],
      detail: ['Discord通知の補助情報', 'preview_file_id / project storage URL', '画像付きembed', 'projects.storage_url', 'previewが無ければテキスト通知のみ']
    },
    {
      title: 'Daily Summary',
      overview: 'DB内のステータス集計を1日1回まとめ、task_type="*" の全体通知 webhook のみに送ります。',
      steps: [['Cron', '毎日9時'], ['Counts', 'status集計'], ['General Webhook', 'task_type="*"'], ['Send', 'summary embed'], ['Log', 'general webhook送信']],
      detail: ['日次の全体通知', 'status counts / project_webhooks', 'summary embed', 'project_webhooks', '全体通知が無い場合は送信せずログに警告']
    },
    {
      title: 'Audit Log',
      overview: 'Discord送信や更新の結果を audit_logs に保存し、管理画面ではPRODUCTIONごとに展開して確認します。',
      steps: [['Send', 'Discord result'], ['Write', 'audit_logs'], ['Group', 'ProjectName'], ['Display', 'PRODUCTION accordion'], ['Inspect', 'message / status']],
      detail: ['送信結果の追跡', 'Discord response / task state', '管理画面の監査表示', 'audit_logs', '失敗結果もログとして残す']
    }
  ]
};
pipelineDocs.en = [
  {
    title: 'Kitsu Poll / Diff / Discord Notification',
    overview: 'The poller reads Kitsu tasks, statuses, and comments, compares them with stored state, and sends Discord updates.',
    steps: [['Kitsu API', 'tasks / statuses / comments'], ['Diff', 'compare SQLite markers'], ['Build', 'embed / preview / mentions'], ['Webhook', 'task type or general'], ['Audit', 'store delivery result']],
    detail: ['Status and comment notifications', 'Kitsu API / DB state', 'Discord embeds and message updates', 'tasks / audit_logs', 'Skips overlapping polls to prevent duplicate sends']
  },
  {
    title: 'Bot Initial Setup',
    overview: 'The admin UI detects the public host, authenticates with a studio admin account, creates a Kitsu bot account, and persists settings.',
    steps: [['Public Host', 'from Host header'], ['Admin Auth', 'email / password'], ['Create Bot', 'Kitsu bot account'], ['Persist', 'settings and env'], ['Reconnect', 'refresh JWT']],
    detail: ['First-run connection setup', 'Public URL / admin credentials', 'kitsu.hostname / kitsu.email', 'settings', 'Failures are shown on Project Setup']
  },
  {
    title: 'Project Setup / Discord Creation',
    overview: 'A selected Kitsu project creates a Discord category, channels, and webhooks from the chosen project type template.',
    steps: [['Select Project', 'Kitsu project'], ['Template', 'CG / Live Action / Anime'], ['Discord', 'category / channel'], ['Webhook', 'per channel'], ['DB', 'projects / project_webhooks']],
    detail: ['Create project notification routing', 'Kitsu project / project type / language', 'Discord objects and webhooks', 'projects / project_webhooks', 'Partial failures appear as WARN/FAIL results']
  },
  {
    title: 'Channel Management',
    overview: 'Configured projects expand into channel management, where template or custom channels can be added and removed immediately.',
    steps: [['Open Project', 'accordion'], ['Add', 'template / custom'], ['Create', 'Discord channel'], ['Webhook', 'project webhook'], ['Delete', 'Discord and DB']],
    detail: ['Adjust live notification targets', 'project_webhooks / Discord category', 'Immediate channel and webhook updates', 'project_webhooks', 'Duplicate names or Discord errors render as page errors']
  },
  {
    title: 'User / Checker Assignment',
    overview: 'User Assignment maps Kitsu users to Discord IDs. Checker Assignment selects a user per task type and resolves IDs during notification building.',
    steps: [['User', 'select Kitsu person'], ['Discord ID', 'user_maps'], ['Checker', 'task type'], ['Resolve', 'from User Assignment'], ['Mention', 'Discord notification']],
    detail: ['Resolve personal mentions', 'Kitsu person / Discord ID / task type', 'resolved Discord ID', 'user_maps / checker_maps', 'Missing IDs show as No ID in the UI']
  },
  {
    title: 'Preview / Storage Links',
    overview: 'Preview images are built from preview_file_id, while Storage Links provide project helper URLs for Discord embeds.',
    steps: [['Task', 'preview_file_id'], ['URL', 'preview-files/{id}.png'], ['Storage', 'project link'], ['Embed', 'image / helper URL'], ['Send', 'Discord message']],
    detail: ['Supplement Discord notifications', 'preview_file_id / project storage URL', 'Image embeds', 'projects.storage_url', 'No preview sends a text-only message']
  },
  {
    title: 'Daily Summary',
    overview: 'Daily status counts are sent once per day only to the general project webhook with task_type="*".',
    steps: [['Cron', 'daily 09:00'], ['Counts', 'status totals'], ['General Webhook', 'task_type="*"'], ['Send', 'summary embed'], ['Log', 'general webhook delivery']],
    detail: ['Daily global notice', 'status counts / project_webhooks', 'summary embed', 'project_webhooks', 'Missing general webhook logs a warning and skips sending']
  },
  {
    title: 'Audit Log',
    overview: 'Discord send and update results are written to audit_logs and grouped by PRODUCTION in the admin UI.',
    steps: [['Send', 'Discord result'], ['Write', 'audit_logs'], ['Group', 'ProjectName'], ['Display', 'PRODUCTION accordion'], ['Inspect', 'message / status']],
    detail: ['Track delivery outcomes', 'Discord response / task state', 'Admin audit view', 'audit_logs', 'Failed deliveries are still recorded']
  }
];

function readInitialLang() {
  try {
    const qs = new URLSearchParams(window.location.search);
    const queryLang = qs.get('lang');
    if (queryLang === 'en' || queryLang === 'ja') {
      localStorage.setItem('admin_lang', queryLang);
      return queryLang;
    }
    const stored = localStorage.getItem('admin_lang');
    if (stored === 'en' || stored === 'ja') {
      return stored;
    }
  } catch (error) {}
  return 'ja';
}

function updateLangInUrl(lang) {
  const qs = new URLSearchParams(window.location.search);
  qs.set('lang', lang);
  const next = `${window.location.pathname}?${qs.toString()}`;
  window.history.replaceState({}, '', next);
}

function markDocsReady() {
  document.body.classList.add('docs-ready');
  const fallback = document.getElementById('docs-fallback');
  if (fallback) {
    fallback.remove();
  }
}

function Section({ section }) {
  return (
    <section className="doc" id={section.id}>
      <div className="kicker">{section.kicker}</div>
      <h1 className="docnum"><span className="n">{section.num}</span>{section.title}</h1>
      <h2 className="docsub">{section.subtitle}</h2>
      {section.intro ? <p className="doc-intro">{section.intro}</p> : null}
      {section.bullets ? (
        <div className="wf wf-pad box">
          <ul style={{ margin: 0, paddingLeft: '20px' }}>
            {section.bullets.map((item) => <li key={item}>{item}</li>)}
          </ul>
        </div>
      ) : null}
      {section.table ? (
        <div className="wf box">
          <table className="adm">
            <thead>
              <tr>{section.table.headers.map((header) => <th key={header}>{header}</th>)}</tr>
            </thead>
            <tbody>
              {section.table.rows.map((row) => (
                <tr key={row[0]}>{row.map((cell, index) => <td key={`${row[0]}-${index}`}>{cell}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  );
}

function PipelineSection({ pipeline, index, lang }) {
  const labels = lang === 'ja'
    ? ['目的', '入力', '出力', '保存先', '失敗時']
    : ['Purpose', 'Input', 'Output', 'Stored In', 'Failure Mode'];
  return (
    <article className="pipeline-detail box">
      <div>
        <div className="kicker">{String(index + 1).padStart(2, '0')} / Pipeline</div>
        <h2 className="docsub" style={{ color: '#fff', fontSize: '1.25rem', marginTop: '8px' }}>{pipeline.title}</h2>
        <p className="doc-intro">{pipeline.overview}</p>
      </div>
      <div className="flow">
        {pipeline.steps.map(([title, detail]) => (
          <div className="flow-step" key={`${pipeline.title}-${title}`}>
            <strong>{title}</strong>
            <span>{detail}</span>
          </div>
        ))}
      </div>
      <div className="kv">
        {pipeline.detail.map((value, detailIndex) => (
          <div key={`${pipeline.title}-${labels[detailIndex]}`}>
            <b>{labels[detailIndex]}</b>
            <span>{value}</span>
          </div>
        ))}
      </div>
    </article>
  );
}

function Site() {
  const [lang, setLang] = useState(readInitialLang);
  const items = sections[lang];
  const [active, setActive] = useState(items[0].id);

  useEffect(() => {
    localStorage.setItem('admin_lang', lang);
    updateLangInUrl(lang);
    setActive(sections[lang][0].id);
  }, [lang]);

  const toggle = () => setLang((current) => (current === 'ja' ? 'en' : 'ja'));

  return (
    <div className="site">
      <aside className="nav">
        <div className="brand">
          Kitsu x Discord Pipeline
          <small>{lang === 'ja' ? 'Current Documentation' : 'Current Documentation'}</small>
        </div>
        <button className="navlink active" style={{ cursor: 'pointer', textAlign: 'left' }} onClick={toggle}>
          <span className="num">SW</span>
          {lang === 'ja' ? 'English' : '日本語'}
        </button>
        <div className="navgroup">
          <div className="navtitle">{lang === 'ja' ? 'Current Features' : 'Current Features'}</div>
          {items.map((item) => (
            <a key={item.id} className={`navlink ${active === item.id ? 'active' : ''}`} onClick={() => setActive(item.id)}>
              <span className="num">{item.num}</span>{item.label}
            </a>
          ))}
        </div>
        <div className="meta">v2.2.0<br />Updated: 2026-05-13</div>
      </aside>
      <main className="body">
        <div className="hero">
          <div className="container">
            <h1>KitsuSync</h1>
            <p className="lead">{lang === 'ja' ? '現在コード上に存在するパイプライン、管理画面、通知先、割り当て、監査、docs 公開方針を一望できる運用ドキュメントです。' : 'Operational documentation for the current pipeline, admin UI, routing, assignments, audit flow, and public-release behavior that exist in the codebase today.'}</p>
            <div className="toc">{items.map((item) => <a key={item.id} onClick={() => setActive(item.id)}>{item.label}</a>)}</div>
          </div>
        </div>
        <div className="container">
          <section className="doc flow-map" id="pipeline-map">
            <div className="kicker">{lang === 'ja' ? 'Pipeline Map' : 'Pipeline Map'}</div>
            <h1 className="docnum"><span className="n">00</span>{lang === 'ja' ? '全体パイプライン' : 'Full Pipeline'}</h1>
            <div className="pipeline-grid">
              {[
                ['Kitsu API', 'tasks / comments / statuses'],
                ['Poll & Diff', 'mutex / sqlite markers'],
                ['Message Builder', 'embed / preview / mentions'],
                ['Discord Webhook', 'task channel or general channel'],
                ['Message Update', 'reuse message id'],
                ['Audit Log', 'grouped by PRODUCTION'],
              ].map(([title, detail]) => <div className="pipe-card box" key={title}><strong>{title}</strong><span>{detail}</span></div>)}
            </div>
          </section>
          <section className="doc" id="pipeline-details">
            <div className="kicker">{lang === 'ja' ? 'Pipeline Details' : 'Pipeline Details'}</div>
            <h1 className="docnum"><span className="n">00B</span>{lang === 'ja' ? '運用パイプライン詳細' : 'Operational Pipeline Details'}</h1>
            <h2 className="docsub">{lang === 'ja' ? '現在の実装に存在する処理だけを図と保存先で整理' : 'Diagrams and storage notes for behavior currently implemented in code'}</h2>
            <div className="pipeline-list">
              {pipelineDocs[lang].map((pipeline, index) => (
                <PipelineSection key={pipeline.title} pipeline={pipeline} index={index} lang={lang} />
              ))}
            </div>
          </section>
          {items.map((item) => <Section key={item.id} section={item} />)}
        </div>
        <footer className="foot">
          <div className="left">Kitsu x Discord Pipeline v2.2</div>
          <div>{lang === 'ja' ? '現行コードベース同期 docs' : 'Docs synchronized to current codebase'}</div>
        </footer>
      </main>
    </div>
  );
}

const rootElement = document.getElementById('root');
if (rootElement) {
  const root = ReactDOM.createRoot(rootElement);
  root.render(<Site />);
  markDocsReady();
}
