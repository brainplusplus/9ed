import { loader } from '@monaco-editor/react';

loader.init().then((monaco) => {
  monaco.languages.register({ id: 'vue', extensions: ['.vue'], aliases: ['Vue', 'vue'] });
  monaco.languages.register({ id: 'svelte', extensions: ['.svelte'], aliases: ['Svelte', 'svelte'] });

  const vueMonarch: import('monaco-editor').languages.IMonarchLanguage = {
    defaultToken: '',
    tokenPostfix: '.vue',
    ignoreCase: true,

    tokenizer: {
      root: [
        [/(<)(template)(\s|>)/, ['delimiter', { token: 'tag', next: '@template' }, '']],
        [/(<)(script)(\s[^>]*)?(>)/, ['delimiter', 'tag', 'attribute.value', { token: 'delimiter', next: '@script' }]],
        [/(<)(style)(\s[^>]*)?(>)/, ['delimiter', 'tag', 'attribute.value', { token: 'delimiter', next: '@style' }]],
        [/<\/?[\w-]+/, 'tag'],
        [/[^<]+/, ''],
      ],

      template: [
        [/(<\/)(template)(>)/, ['delimiter', 'tag', { token: 'delimiter', next: '@pop' }]],
        [/\{\{/, { token: 'delimiter.bracket', next: '@interpolation' }],
        [/(v-[\w-]+|@[\w.-]+|:[\w.-]+|#[\w.-]+)/, 'attribute.name'],
        [/"[^"]*"/, 'attribute.value'],
        [/'[^']*'/, 'attribute.value'],
        [/<\/?[\w-]+/, 'tag'],
        [/\/?>/, 'delimiter'],
        [/=/, 'delimiter'],
        [/[\w-]+/, 'attribute.name'],
        [/[^<{]+/, ''],
      ],

      interpolation: [
        [/\}\}/, { token: 'delimiter.bracket', next: '@pop' }],
        [/./, 'variable'],
      ],

      script: [
        [/(<\/)(script)(>)/, ['delimiter', 'tag', { token: 'delimiter', next: '@pop' }]],
        [/\/\/.*$/, 'comment'],
        [/\/\*/, { token: 'comment', next: '@blockComment' }],
        [/"([^"\\]|\\.)*"/, 'string'],
        [/'([^'\\]|\\.)*'/, 'string'],
        [/`([^`\\]|\\.)*`/, 'string'],
        [/\b(import|export|from|const|let|var|function|return|if|else|for|while|class|extends|new|this|typeof|interface|type|enum|async|await|default|switch|case|break|continue|throw|try|catch|finally)\b/, 'keyword'],
        [/\b(true|false|null|undefined|NaN|Infinity)\b/, 'constant'],
        [/\b\d[\d_]*(\.\d[\d_]*)?\b/, 'number'],
        [/[{}()\[\]]/, 'bracket'],
        [/[;,.]/, 'delimiter'],
        [/[a-zA-Z_$][\w$]*/, 'identifier'],
        [/./, ''],
      ],

      style: [
        [/(<\/)(style)(>)/, ['delimiter', 'tag', { token: 'delimiter', next: '@pop' }]],
        [/\/\*/, { token: 'comment', next: '@blockComment' }],
        [/[.#][\w-]+/, 'tag'],
        [/@[\w-]+/, 'keyword'],
        [/[\w-]+(?=\s*:)/, 'attribute.name'],
        [/:/, 'delimiter'],
        [/;/, 'delimiter'],
        [/[{}()]/, 'bracket'],
        [/"[^"]*"/, 'string'],
        [/'[^']*'/, 'string'],
        [/\b\d[\d.]*(%|px|em|rem|vh|vw|s|ms)?\b/, 'number'],
        [/./, ''],
      ],

      blockComment: [
        [/\*\//, { token: 'comment', next: '@pop' }],
        [/./, 'comment'],
      ],
    },
  };

  const svelteMonarch: import('monaco-editor').languages.IMonarchLanguage = {
    defaultToken: '',
    tokenPostfix: '.svelte',
    ignoreCase: false,

    tokenizer: {
      root: [
        [/(<)(script)(\s[^>]*)?(>)/, ['delimiter', 'tag', 'attribute.value', { token: 'delimiter', next: '@script' }]],
        [/(<)(style)(\s[^>]*)?(>)/, ['delimiter', 'tag', 'attribute.value', { token: 'delimiter', next: '@style' }]],
        [/\{#(if|each|await|key)\b/, { token: 'keyword', next: '@svelteBlock' }],
        [/\{:(else|then|catch)\b/, 'keyword'],
        [/\{\/(if|each|await|key)\}/, 'keyword'],
        [/\{@(html|debug|const)\b/, { token: 'keyword', next: '@svelteExpr' }],
        [/\{/, { token: 'delimiter.bracket', next: '@svelteExpr' }],
        [/(on:|bind:|class:|style:|use:|transition:|animate:|in:|out:)[\w|]+/, 'attribute.name'],
        [/<\/?[\w-]+/, 'tag'],
        [/\/?>/, 'delimiter'],
        [/=/, 'delimiter'],
        [/"[^"]*"/, 'attribute.value'],
        [/'[^']*'/, 'attribute.value'],
        [/[\w-]+/, 'attribute.name'],
        [/[^<{]+/, ''],
      ],

      svelteBlock: [
        [/\}/, { token: 'keyword', next: '@pop' }],
        [/./, 'variable'],
      ],

      svelteExpr: [
        [/\}/, { token: 'delimiter.bracket', next: '@pop' }],
        [/./, 'variable'],
      ],

      script: [
        [/(<\/)(script)(>)/, ['delimiter', 'tag', { token: 'delimiter', next: '@pop' }]],
        [/\/\/.*$/, 'comment'],
        [/\/\*/, { token: 'comment', next: '@blockComment' }],
        [/"([^"\\]|\\.)*"/, 'string'],
        [/'([^'\\]|\\.)*'/, 'string'],
        [/`([^`\\]|\\.)*`/, 'string'],
        [/\$:/, 'keyword'],
        [/\b(import|export|from|const|let|var|function|return|if|else|for|while|class|extends|new|this|typeof|interface|type|async|await|default|switch|case|break|continue|throw|try|catch|finally)\b/, 'keyword'],
        [/\b(true|false|null|undefined)\b/, 'constant'],
        [/\b\d[\d_]*(\.\d[\d_]*)?\b/, 'number'],
        [/[{}()\[\]]/, 'bracket'],
        [/[;,.]/, 'delimiter'],
        [/[a-zA-Z_$][\w$]*/, 'identifier'],
        [/./, ''],
      ],

      style: [
        [/(<\/)(style)(>)/, ['delimiter', 'tag', { token: 'delimiter', next: '@pop' }]],
        [/\/\*/, { token: 'comment', next: '@blockComment' }],
        [/[.#][\w-]+/, 'tag'],
        [/@[\w-]+/, 'keyword'],
        [/[\w-]+(?=\s*:)/, 'attribute.name'],
        [/:/, 'delimiter'],
        [/;/, 'delimiter'],
        [/[{}()]/, 'bracket'],
        [/"[^"]*"/, 'string'],
        [/'[^']*'/, 'string'],
        [/\b\d[\d.]*(%|px|em|rem|vh|vw|s|ms)?\b/, 'number'],
        [/./, ''],
      ],

      blockComment: [
        [/\*\//, { token: 'comment', next: '@pop' }],
        [/./, 'comment'],
      ],
    },
  };

  monaco.languages.setMonarchTokensProvider('vue', vueMonarch);
  monaco.languages.setMonarchTokensProvider('svelte', svelteMonarch);

  monaco.languages.setLanguageConfiguration('vue', {
    comments: { lineComment: '//', blockComment: ['/*', '*/'] },
    brackets: [['<', '>'], ['{', '}'], ['(', ')'], ['[', ']']],
    autoClosingPairs: [
      { open: '{', close: '}' },
      { open: '[', close: ']' },
      { open: '(', close: ')' },
      { open: '<', close: '>' },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
      { open: '`', close: '`' },
    ],
    surroundingPairs: [
      { open: '{', close: '}' },
      { open: '[', close: ']' },
      { open: '(', close: ')' },
      { open: '<', close: '>' },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
  });

  monaco.languages.setLanguageConfiguration('svelte', {
    comments: { lineComment: '//', blockComment: ['/*', '*/'] },
    brackets: [['{', '}'], ['(', ')'], ['[', ']'], ['<', '>']],
    autoClosingPairs: [
      { open: '{', close: '}' },
      { open: '[', close: ']' },
      { open: '(', close: ')' },
      { open: '<', close: '>' },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
      { open: '`', close: '`' },
    ],
    surroundingPairs: [
      { open: '{', close: '}' },
      { open: '[', close: ']' },
      { open: '(', close: ')' },
      { open: '<', close: '>' },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
  });
});
