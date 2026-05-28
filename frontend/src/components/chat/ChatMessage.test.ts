import { describe, expect, it } from 'vitest';

import { describeToolInput } from './ChatMessage';

describe('describeToolInput', () => {
  it('shows final browser URL from tool output when it differs from input', () => {
    const summary = describeToolInput({
      toolCallId: 'tool-1',
      title: '9ed_browser_goto',
      kind: 'browser',
      status: 'completed',
      rawInput: JSON.stringify({ action: 'goto', url: 'https://www.detik.com' }),
      content: [
        'Opened url=https://www.detik.com/',
        '',
        JSON.stringify({
          url: 'https://news.detik.com/berita/d-8508470/termasuk-mahasiswa-ugm-ini-identitas',
          title: 'Termasuk Mahasiswa...',
        }),
      ].join('\n'),
    });

    expect(summary).toBe('https://www.detik.com -> https://news.detik.com/berita/d-8508470/termasuk-mahasiswa-ugm-ini-identitas');
  });
});
