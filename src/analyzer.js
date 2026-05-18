const Anthropic = require('@anthropic-ai/sdk');

const client = new Anthropic();

const SYSTEM_PROMPT = `You are a screen monitoring assistant. You receive a screenshot of a single window or application on the user's computer and produce two things:

1. "summary": a concise 1-3 sentence description of what is currently on screen — what app or content it is and what the user appears to be doing. Be literal; describe only what is actually visible.

2. "reminders": a list of genuinely actionable items visible on screen — deadlines, meetings, tasks, unanswered messages, follow-ups. Return an empty list if nothing needs action. Never invent items.

For each reminder set "remindInMinutes" to how many minutes from now the user should be alerted. If the screen names a specific time, compute the offset from the provided current time. If there is no clear timing, use 0 (the reminder is shown in the list but not scheduled as a notification).`;

const SCHEMA = {
  type: 'object',
  properties: {
    summary: { type: 'string' },
    reminders: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          text: { type: 'string' },
          remindInMinutes: { type: 'number' },
        },
        required: ['text', 'remindInMinutes'],
        additionalProperties: false,
      },
    },
  },
  required: ['summary', 'reminders'],
  additionalProperties: false,
};

async function analyzeScreenshot(imageBase64) {
  try {
    const response = await client.messages.create({
      model: 'claude-haiku-4-5',
      max_tokens: 1024,
      system: [
        { type: 'text', text: SYSTEM_PROMPT, cache_control: { type: 'ephemeral' } },
      ],
      output_config: {
        format: { type: 'json_schema', schema: SCHEMA },
      },
      messages: [
        {
          role: 'user',
          content: [
            {
              type: 'image',
              source: { type: 'base64', media_type: 'image/jpeg', data: imageBase64 },
            },
            {
              type: 'text',
              text: `Current time: ${new Date().toLocaleString()}. Analyze this screenshot.`,
            },
          ],
        },
      ],
    });

    const textBlock = response.content.find((b) => b.type === 'text');
    const parsed = JSON.parse(textBlock.text);
    return {
      ok: true,
      summary: parsed.summary,
      reminders: parsed.reminders || [],
      usage: {
        inputTokens: response.usage.input_tokens,
        outputTokens: response.usage.output_tokens,
      },
    };
  } catch (err) {
    return { ok: false, error: err.message };
  }
}

module.exports = { analyzeScreenshot };
