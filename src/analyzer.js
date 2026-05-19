const Anthropic = require('@anthropic-ai/sdk');

let client = null;

function setApiKey(key) {
  const trimmed = (key || '').trim();
  client = trimmed ? new Anthropic({ apiKey: trimmed }) : null;
}

function hasApiKey() {
  return !!client;
}

// Fall back to an environment variable when one is present.
if (process.env.ANTHROPIC_API_KEY) setApiKey(process.env.ANTHROPIC_API_KEY);

const FRAME_PROMPT = `You are a screen monitoring assistant. You receive a screenshot taken during a screen recording and produce two things:

1. "summary": a concise 1-2 sentence description of what is on screen right now — what app or content it is and what the user appears to be doing. Be literal; describe only what is actually visible.

2. "reminders": a list of genuinely actionable items visible on screen — deadlines, meetings, tasks, unanswered messages, follow-ups. Return an empty list if nothing needs action. Never invent items.

For each reminder set "remindInMinutes" to how many minutes from now the user should be alerted. If the screen names a specific time, compute the offset from the provided current time. If there is no clear timing, use 0.`;

const ROLLUP_PROMPT = `You are given a chronological list of observations captured at intervals during a screen recording. Produce two things:

1. "summary": a single overview paragraph describing what happened across the whole recording, start to finish.

2. "reminders": the consolidated list of actionable items worth following up on. Merge duplicates. Return an empty list if there is nothing actionable.

For each reminder set "remindInMinutes" to how many minutes from now the user should be alerted, or 0 if there is no clear timing.`;

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

const NO_KEY = { ok: false, error: 'No Anthropic API key set — add one in the app settings.' };

function usageOf(response) {
  return {
    inputTokens: response.usage.input_tokens,
    outputTokens: response.usage.output_tokens,
  };
}

async function analyzeScreenshot(imageBase64) {
  if (!client) return NO_KEY;
  try {
    const response = await client.messages.create({
      model: 'claude-haiku-4-5',
      max_tokens: 1024,
      system: [{ type: 'text', text: FRAME_PROMPT, cache_control: { type: 'ephemeral' } }],
      output_config: { format: { type: 'json_schema', schema: SCHEMA } },
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
      usage: usageOf(response),
    };
  } catch (err) {
    return { ok: false, error: err.message };
  }
}

async function summarizeSession(summaries) {
  if (!client) return NO_KEY;
  try {
    if (!summaries || summaries.length === 0) {
      return {
        ok: true,
        summary: 'No screen activity was analyzed during this recording.',
        reminders: [],
        usage: null,
      };
    }

    const observations = summaries.map((s, i) => `${i + 1}. ${s}`).join('\n');
    const response = await client.messages.create({
      model: 'claude-haiku-4-5',
      max_tokens: 1024,
      system: [{ type: 'text', text: ROLLUP_PROMPT, cache_control: { type: 'ephemeral' } }],
      output_config: { format: { type: 'json_schema', schema: SCHEMA } },
      messages: [
        {
          role: 'user',
          content: `Current time: ${new Date().toLocaleString()}.\n\nObservations captured during the recording, in order:\n\n${observations}`,
        },
      ],
    });

    const textBlock = response.content.find((b) => b.type === 'text');
    const parsed = JSON.parse(textBlock.text);
    return {
      ok: true,
      summary: parsed.summary,
      reminders: parsed.reminders || [],
      usage: usageOf(response),
    };
  } catch (err) {
    return { ok: false, error: err.message };
  }
}

module.exports = { analyzeScreenshot, summarizeSession, setApiKey, hasApiKey };
