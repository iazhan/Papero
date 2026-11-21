/* Papero 官网的交互：语言、主题、平台、进场动画。
 *
 * 中文是 HTML 里的原文，切到英文时用下面这份字典替换 innerHTML，
 * 切回来再从原文恢复——这样只有一份文案源，不会两边改不同步。
 *
 * 首屏那张截图画的是真程序，四种组合各一张（中/英 × 浅/深），按当前的
 * 语言与主题挑。下载地址与版本号也不写死在 HTML 里：地址走 GitHub 的
 * /releases/latest/download/<产物名>，版本号从 Releases API 现取，取不到就
 * 用 HTML 里写的那份。
 */
(function () {
  document.documentElement.classList.add('js');

  var REPO = 'https://github.com/iazhan/Papero';

  var EN = {
    'nav.principles': 'Rules',
    'nav.features': 'Features',
    'nav.formats': 'Formats',
    'nav.dl': 'Download',

    'a11y.lang': 'Switch to English',
    'a11y.theme': 'Toggle light or dark theme',

    'hero.eyebrow': 'Windows 10+ · one executable · MIT',
    'hero.title': 'Notes, kept<br>in a folder.',
    'hero.lede': 'Papero is Esperanto for paper. It puts Markdown, PDF, Word, images and whiteboards in one window — and draws every pixel natively in Go on the GPU. No WebView, no Electron, no JavaScript.',
    'hero.cta1': 'Download for Windows',
    'hero.cta2': 'Read the source on GitHub',
    'hero.meta': 'v{v} · ~25 MB · no installer · Windows only, verified',

    'trait.1': 'No WebView',
    'trait.2': 'No Electron',
    'trait.3': 'No JavaScript',
    'trait.4': 'One executable',
    'trait.5': 'Follows your desktop theme',

    'p.label': 'Design rules',
    'p.title': 'Three rules decide how it looks',
    'p.1.h': 'A document is a file',
    'p.1.b': 'A vault is an ordinary folder, and a note is an ordinary <code>.md</code>. Nothing to import, nothing to export. Git can track them, your file search can find them, and deleting Papero leaves them where they are.',
    'p.2.h': 'Drawn, not rendered',
    'p.2.b': 'Window, layout, editor and whiteboard are all painted by Go on the GPU. When nothing moves it draws nothing — so it starts fast and stays small.',
    'p.3.h': 'One executable',
    'p.3.b': 'No installer dependencies, no runtime, no node_modules. Download it, double-click, and it opens. Uninstalling means deleting one file.',

    'f.label': 'Features',
    'f.title': "Everything it needs. Nothing it doesn't.",
    'f.1.h': 'Block editor',
    'f.1.b': 'Click any block to edit its Markdown source — the caret lands on the exact character you clicked and the rest keeps its layout. Type <kbd>/</kbd> in an empty block to insert code, math or a table.',
    'f.2.h': 'Wiki links',
    'f.2.b': 'Type <code>[[</code> to complete across the whole vault. Renaming rewrites every reference; deleting tells you which notes will break.',
    'f.3.h': 'Graph',
    'f.3.b': 'Laid out from your links, coloured by folder. Double-click a node to open the note — embedded in the window, or in one of its own.',
    'f.4.h': 'PDF reading',
    'f.4.b': 'A pure-Go content-stream parser re-flows text and images at their original positions — selectable, searchable, highlightable.',
    'f.5.h': 'Whiteboard',
    'f.5.b': 'Opens and edits <code>.excalidraw</code> natively and stays compatible with Excalidraw itself, so the file works anywhere.',
    'f.6.h': 'TeX math',
    'f.6.b': 'Inline math sits on the baseline, display math centres itself. Fraction bars, radicals, sub- and superscripts, summation limits — all typeset.',
    'f.7.h': 'Sync',
    'f.7.b': 'Folder, Git, vault repo or WebDAV — a file-level three-way merge that never swallows your text in a conflict.',
    'f.8.h': 'Three colours',
    'f.8.b': 'Background, foreground, accent: the whole interface derives from those three. Switch the UI between Chinese and English at any time.',

    'm.label': 'Formats',
    'm.title': 'More than Markdown',
    'm.th1': 'Type',
    'm.th2': 'What it does',
    'm.1': 'Split (source + live preview), preview only, or source only. Syntax highlighting, clickable tasks, inline images, math and tables.',
    'm.2k': 'Images',
    'm.2': 'PNG / JPEG / GIF / WebP / BMP / SVG. Fit to window, 1:1, zoom — large images stay sharp.',
    'm.3': 'Parsed in pure Go and re-flowed at the original layout. Paging, zoom, text selection, highlight annotations.',
    'm.4': '<code>.docx</code> reads as Markdown layout: headings, bold and italic, lists, tables.',
    'm.5k': 'Plain text',
    'm.5': 'Code, logs, CSV — highlighted by extension.',
    'm.6k': 'Whiteboard',
    'm.6': 'Opens and edits <code>.excalidraw</code> natively, compatible both ways.',

    'ft.line': 'A local-first Markdown knowledge base. Documents are files; the interface is drawn.',
    'ft.product': 'Product',
    'ft.res': 'Resources',
    'ft.credit': 'Credits',
    'ft.changelog': 'Changelog',
    'ft.docs': 'Docs',
    'ft.fine': 'MIT licensed · Drawn with Go and MyGo · Icons from IconMCP and Lucide'
  };

  /* ---------- 版本 ---------- */

  // 和 mygo.json / appVersion 同步的那份兜底；能连上 GitHub 就用最新的覆盖。
  var version = '0.2.1';

  function applyVersion() {
    Array.prototype.forEach.call(document.querySelectorAll('[data-version]'), function (el) {
      el.textContent = version;
    });
  }

  // 官网不该因为 GitHub 挂了就打不开，所以取不到就安静地留着兜底值。
  function fetchVersion() {
    if (!window.fetch) return;
    fetch('https://api.github.com/repos/iazhan/Papero/releases/latest', {
      headers: { Accept: 'application/vnd.github+json' }
    }).then(function (r) {
      return r.ok ? r.json() : null;
    }).then(function (d) {
      if (!d || !d.tag_name) return;
      var v = String(d.tag_name).replace(/^v/, '');
      if (!v || v === version) return;
      version = v;
      applyVersion();
      applyLang(lang);
    }).catch(function () {});
  }

  /* ---------- 平台 ---------- */

  var ASSET = {
    windows: 'papero-windows-amd64.exe',
    macos: 'papero-darwin-arm64',
    linux: 'papero-linux-amd64'
  };
  var CTA = {
    zh: { windows: '下载 Windows 版', macos: '下载 macOS 版', linux: '下载 Linux 版' },
    en: { windows: 'Download for Windows', macos: 'Download for macOS', linux: 'Download for Linux' }
  };
  // 首屏那行小字跟着平台走：Mac 用户看到「Windows 10+」会以为走错了门。
  var EYEBROW = {
    zh: {
      windows: 'Windows 10+ · 一个可执行文件 · MIT',
      macos: 'macOS · Apple 芯片 · 实验性',
      linux: 'Linux · amd64 · 实验性'
    },
    en: {
      windows: 'Windows 10+ · one executable · MIT',
      macos: 'macOS · Apple silicon · experimental',
      linux: 'Linux · amd64 · experimental'
    }
  };

  function detectOS() {
    var ua = (navigator.userAgent || '').toLowerCase();
    var platform = (navigator.userAgentData && navigator.userAgentData.platform || navigator.platform || '').toLowerCase();
    if (/mac|iphone|ipad/.test(platform) || /mac os x|macintosh|iphone|ipad/.test(ua)) return 'macos';
    if (/linux|android/.test(platform) || /linux|x11/.test(ua)) return 'linux';
    return 'windows';
  }

  var OS = detectOS();
  var ctaDownload = document.getElementById('ctaDownload');
  var eyebrow = document.getElementById('eyebrow');

  function applyDownload() {
    // latest/download/<名字> 永远指向最新一版的那个文件，发版换代不用改这里。
    var href = REPO + '/releases/latest/download/' + ASSET[OS];
    Array.prototype.forEach.call(document.querySelectorAll('[data-download]'), function (a) {
      a.setAttribute('href', href);
    });
    var key = lang === 'en' ? 'en' : 'zh';
    if (ctaDownload) ctaDownload.textContent = CTA[key][OS];
    if (eyebrow) eyebrow.textContent = EYEBROW[key][OS];
  }

  /* ---------- 语言 ---------- */

  var textNodes = Array.prototype.slice.call(document.querySelectorAll('[data-i18n]'));
  var labelNodes = Array.prototype.slice.call(document.querySelectorAll('[data-i18n-label]'));
  var shots = Array.prototype.slice.call(document.querySelectorAll('[data-src-en]'));

  var zhText = new Map(textNodes.map(function (n) { return [n.dataset.i18n, n.innerHTML]; }));
  var zhLabel = new Map(labelNodes.map(function (n) { return [n.dataset.i18nLabel, n.getAttribute('aria-label')]; }));

  // 截图的原始地址与 alt 只记一次，切回来时还原
  shots.forEach(function (img) {
    img.dataset.srcZh = img.getAttribute('src');
    img.dataset.altZh = img.getAttribute('alt') || '';
    img.dataset.altEn = img.dataset.altEn || img.dataset.altZh;
  });

  var zhTitle = document.title;
  var descEl = document.querySelector('meta[name="description"]');
  var zhDesc = descEl ? descEl.getAttribute('content') : '';

  var langBtn = document.getElementById('lang');
  var lang = 'zh';

  // 截图跟着语言和明暗走：中文看中文界面，深色看深色界面。
  function syncShots() {
    var en = lang === 'en';
    var dark = document.documentElement.dataset.theme === 'dark';
    shots.forEach(function (img) {
      var src = img.dataset.srcZh;
      if (en && dark) src = img.dataset.srcEnDark || src;
      else if (en) src = img.dataset.srcEn || src;
      else if (dark) src = img.dataset.srcDark || src;
      if (src && img.getAttribute('src') !== src) img.setAttribute('src', src);
      img.setAttribute('alt', en ? img.dataset.altEn : img.dataset.altZh);
    });
  }

  function applyLang(next) {
    lang = next;
    var en = next === 'en';
    var dict = en ? EN : null;

    textNodes.forEach(function (n) {
      var key = n.dataset.i18n;
      var value = dict ? dict[key] : zhText.get(key);
      if (value === undefined) {
        if (dict) console.warn('[papero] missing translation:', key);
        value = zhText.get(key);
      }
      value = value.replace(/\{v\}/g, version);
      if (n.innerHTML !== value) n.innerHTML = value;
    });

    labelNodes.forEach(function (n) {
      var key = n.dataset.i18nLabel;
      var value = dict ? EN[key] : zhLabel.get(key);
      n.setAttribute('aria-label', value || zhLabel.get(key) || '');
    });

    document.documentElement.lang = en ? 'en' : 'zh-CN';
    document.title = en ? 'Papero — a local-first Markdown knowledge base' : zhTitle;
    if (descEl) {
      descEl.setAttribute('content', en
        ? 'Papero is Esperanto for paper: a Markdown-first knowledge base that also opens PDF, Word, images and Excalidraw boards. Everything is drawn natively in Go — no WebView, no Electron.'
        : zhDesc);
    }

    langBtn.textContent = en ? '中' : 'EN';
    langBtn.setAttribute('aria-label', en ? '切换到中文' : 'Switch to English');

    applyDownload();
    applyTheme();
    syncShots();
    try { localStorage.setItem('papero-lang', next); } catch (e) {}
  }

  langBtn.addEventListener('click', function () {
    applyLang(lang === 'en' ? 'zh' : 'en');
  });

  /* ---------- 主题 ---------- */

  var THEME_KEY = 'papero-theme';
  var mq = window.matchMedia('(prefers-color-scheme: dark)');
  var pref = 'auto';
  try { pref = localStorage.getItem(THEME_KEY) || 'auto'; } catch (e) {}

  var themeBtn = document.getElementById('theme');
  var metas = Array.prototype.slice.call(document.querySelectorAll('meta[name="theme-color"]'));

  function resolved(p) {
    return p === 'auto' ? (mq.matches ? 'dark' : 'light') : p;
  }

  function applyTheme() {
    var theme = resolved(pref);
    document.documentElement.dataset.theme = theme;

    var dark = theme === 'dark';
    themeBtn.setAttribute('aria-label', lang === 'en'
      ? (dark ? 'Switch to light theme' : 'Switch to dark theme')
      : (dark ? '切换到浅色' : '切换到深色'));

    if (metas.length) {
      metas.forEach(function (m, i) {
        if (i === 0) {
          m.removeAttribute('media');
          m.setAttribute('content', dark ? '#1b1b1a' : '#ffffff');
        } else {
          m.remove();
        }
      });
      metas = [metas[0]];
    }

    syncShots();
  }

  function setPref(next) {
    pref = next;
    try { localStorage.setItem(THEME_KEY, next); } catch (e) {}
    applyTheme();
  }

  themeBtn.addEventListener('click', function () {
    setPref(resolved(pref) === 'dark' ? 'light' : 'dark');
  });

  if (mq.addEventListener) {
    mq.addEventListener('change', function () { if (pref === 'auto') applyTheme(); });
  }

  /* ---------- 进场动画 ---------- */

  var reveals = Array.prototype.slice.call(document.querySelectorAll('.reveal'));

  ['.features', '.principles'].forEach(function (sel) {
    var group = document.querySelector(sel);
    if (!group) return;
    Array.prototype.forEach.call(group.children, function (el, i) {
      if (el.classList.contains('reveal')) el.style.setProperty('--d', (i % 4) * 70 + 'ms');
    });
  });

  if ('IntersectionObserver' in window) {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (!e.isIntersecting) return;
        e.target.classList.add('in');
        io.unobserve(e.target);
      });
    }, { rootMargin: '0px 0px -6% 0px', threshold: 0.05 });
    reveals.forEach(function (el) { io.observe(el); });
  } else {
    reveals.forEach(function (el) { el.classList.add('in'); });
  }

  /* ---------- 顶栏描边 ---------- */

  var header = document.getElementById('header');
  function onScroll() {
    header.classList.toggle('is-scrolled', window.scrollY > 8);
  }
  window.addEventListener('scroll', onScroll, { passive: true });
  onScroll();

  /* ---------- 启动 ---------- */

  var saved = 'zh';
  try { saved = localStorage.getItem('papero-lang') || 'zh'; } catch (e) {}

  applyVersion();
  applyTheme();
  applyLang(saved === 'en' ? 'en' : 'zh');
  fetchVersion();
})();
