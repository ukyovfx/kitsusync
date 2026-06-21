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
      bullets: ['Bot 初期設定では公開ホストから Kitsu hostname を自動検出し、スタジオ管理者メール / パスワードだけで Bot アカウントを作成', 'Project Setup は Kitsu プロジェクトを選び、Discord カテゴリとテキストチャンネル群を作成', 'プロジェクトタイプは CG / VFX をサポート', '作成したチャンネルには webhook を発行し、project_webhooks テーブルへ保存', '既存プロジェクトは setup 画面から削除とチャンネル追加 / 削除を行う']
    },
    {
      id: 'channels',
      num: '04',
      label: 'チャンネル',
      kicker: 'Discord Structure',
      title: 'チャンネル構成と通知先',
      subtitle: 'テンプレートと全体通知の扱い',
      intro: '各プロジェクトには task type ごとのチャンネルと、`*` を担当する全体通知チャンネルを持てます。daily summary はこの全体通知 webhook を優先して送ります。',
      bullets: ['CG / VFX テンプレートを保持', 'テンプレート外の Custom チャンネルも追加可能', 'task type が一致する webhook が優先され、未一致なら `*` の全体通知 webhook へフォールバック', 'チャンネル追加 / 削除は Discord 側へ即時反映']
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
      bullets: ['監査ログは時刻、対象、task type、状態、message ID、結果を表示', 'daily summary は status 集計を embed 化して送信', 'summary の送信先は main webhook ではなく project の全体通知 webhook を優先', '一般 webhook が無い場合のみ main webhook を利用']
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
      bullets: ['secrets は画面に平文表示しない', 'Webhook URL は docs へ露出しない', 'DCC launcher / dcc-tools は公開版から削除', '管理画面は Kitsu manager / admin ログインで保護']
    }
  ],
  en: []
};
sections.en = [
  { ...sections.ja[0], label: 'Overview', kicker: 'System Overview', title: 'Kitsu x Discord Pipeline', subtitle: 'Current production architecture and responsibilities', intro: 'This app polls Kitsu/Zou, detects status or comment changes, and sends Discord notifications. The public build removes DCC integrations while keeping setup, assignment, audit, and preview workflows needed for operations.', bullets: ['Fetches tasks, task statuses, entities, projects, task types, persons, and comments from Kitsu APIs', 'Uses SQLite state to detect changes', 'Sends Discord embeds and message updates', 'Adds preview image URLs when preview_file_id is present', 'Exposes audit logs and daily summary through the admin UI'] },
  { ...sections.ja[1], label: 'Routes', subtitle: 'Current nginx and bot-app entry points', table: { headers: ['Path','Purpose'], rows: [['/','Kitsu application'],['/bot/login','Admin login'],['/bot/setup','Project setup and bot bootstrap'],['/bot/admin','Admin home'],['/bot/admin/users','User assignment'],['/bot/admin/checkers','Checker assignment'],['/bot/admin/bot','Bot settings'],['/bot/admin/audit','Audit log']] } },
  { ...sections.ja[2], label: 'Setup', subtitle: 'First-run workflow for administrators', bullets: ['Bot bootstrap auto-detects the public Kitsu hostname and only asks for studio admin email and password', 'Project Setup creates Discord categories and text channels from a selected Kitsu project', 'Supports CG / VFX, Live Action, and Anime project types', 'Creates webhooks for each channel and stores them in project_webhooks', 'Existing projects can be deleted or have channels added and removed from the setup page'] },
  { ...sections.ja[3], label: 'Channels', subtitle: 'Templates and general notification routing', intro: 'Each project can have task-type channels plus a general channel mapped to `*`. Daily summary prefers these general webhooks.', bullets: ['Keeps templates for CG / VFX, Live Action, and Anime', 'Supports custom channels in addition to templates', 'Task-specific webhooks are preferred; `*` general webhooks are the fallback', 'Channel add and delete actions apply to Discord immediately'] },
  { ...sections.ja[4], label: 'Assignments', subtitle: 'How individual mentions are resolved', bullets: ['User Assignment stores Kitsu user to Discord ID mappings', 'The Kitsu bot account is excluded from assignment candidates', 'Checker Assignment stores a user per task type and normally resolves the Discord ID from User Assignment', 'Edit mode can override the Resolved ID directly', 'Missing IDs are shown as No ID in the UI'] },
  { ...sections.ja[5], label: 'Notifications', subtitle: 'Diff detection, send, and update pipeline', bullets: ['Polls Kitsu and filters only tasks with meaningful changes', 'Embed colors follow the Kitsu status color', 'Existing Discord message IDs are reused when updating the same task', 'Audit log entries are written for status or comment deliveries', 'A mutex protects the poller from duplicate send cycles'] },
  { ...sections.ja[6], label: 'Preview', subtitle: 'Image preview and helper links', bullets: ['Preview URL format is `{KITSU_PUBLIC_HOSTNAME or KITSU_HOSTNAME}/api/pictures/thumbnails/preview-files/{id}.png`', 'If no preview is available, the message is sent without an image', 'Storage Links store optional Drive or file links per project', 'Storage Links are helper URLs for embeds, not webhook settings'] },
  { ...sections.ja[7], label: 'Audit', subtitle: 'Delivery visibility and daily rollup', bullets: ['Audit Log shows time, target, task type, status, message ID, and result', 'Daily summary aggregates task statuses into one embed', 'Summary delivery prefers project general webhooks over the main webhook', 'The main webhook is only used as a fallback when no general webhook exists'] },
  { ...sections.ja[8], label: 'Data', subtitle: 'Main tables and persisted settings', table: { headers: ['Item','Purpose'], rows: [['projects','Maps Kitsu projects to Discord categories'],['project_webhooks','Stores channel name, task type, webhook URL, and Discord channel ID'],['user_maps','Stores Kitsu user to Discord ID mappings'],['checker_maps','Stores checker user per task type and resolved IDs'],['audit_logs','Stores Discord delivery results'],['settings','Stores kitsu.hostname, kitsu.email, discord.guildID, and related values']] } },
  { ...sections.ja[9], label: 'Security', subtitle: 'What stays and what is removed in the public build', bullets: ['Secrets are not shown in plain text', 'Webhook URLs are not exposed in docs', 'DCC launcher integrations and dcc-tools are removed', 'The admin UI is protected by Kitsu manager/admin login'] }
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
            <h1>Kitsu <em>x</em> Discord</h1>
            <p className="lead">{lang === 'ja' ? '現在コード上に存在するパイプライン、管理画面、通知先、割り当て、監査、docs 公開方針をまとめた運用ドキュメントです。' : 'Operational documentation for the current pipeline, admin UI, routing, assignments, audit flow, and public-release behavior that exist in the codebase today.'}</p>
            <div className="toc">{items.map((item) => <a key={item.id} onClick={() => setActive(item.id)}>{item.label}</a>)}</div>
          </div>
        </div>
        <div className="container"><Section section={items.find((item) => item.id === active) || items[0]} /></div>
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
}
