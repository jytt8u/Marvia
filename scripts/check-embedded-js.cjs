// Проверка синтаксиса скриптов, вшитых в страницы панели и окна Windows.
//
// Скрипт живёт внутри HTML, и ни Go, ни сборка его не разбирают: опечатка в
// кавычке доезжала до продавца, и панель открывалась пустой, с одной ошибкой
// в консоли. Здесь каждый <script> разбирается как настоящий код — без
// выполнения, — а метка темы /*%LOOK%*/ подменяется пустым местом, как при
// отдаче.
//
// Запуск: node scripts/check-embedded-js.cjs

const fs = require('node:fs');
const { Script } = require('node:vm');

const pages = [
  'internal/panel/web/index.html',
  'internal/panel/web/buyer.html',
  'cmd/marvia-windows/ui/app.html',
];

let failed = 0;
for (const page of pages) {
  if (!fs.existsSync(page)) continue;
  const html = fs.readFileSync(page, 'utf8');
  const scripts = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)];
  scripts.forEach((m, i) => {
    const attrs = m[1];
    if (/\bsrc=/.test(attrs) || /type="(?!module|text\/javascript)/.test(attrs)) return;
    // Шаблоны Go внутри скрипта ({{ .Nonce }} и т. п.) — не наш синтаксис.
    const code = m[2].replace(/\{\{[^}]*\}\}/g, '0');
    try {
      new Script(code, { filename: `${page} <script #${i + 1}>` });
    } catch (err) {
      failed++;
      console.error(`${page}, скрипт ${i + 1}: ${err.message}`);
      if (err.stack) console.error(err.stack.split('\n').slice(0, 3).join('\n'));
    }
  });
}

if (failed) {
  console.error(`синтаксические ошибки во вшитых скриптах: ${failed}`);
  process.exit(1);
}
console.log('вшитые скрипты разбираются без ошибок');
