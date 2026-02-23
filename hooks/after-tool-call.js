/**
 * BrowserNERD Flight Recorder Hook
 * 
 * Automatically captures errors returned by tools and injects them
 * back to the user via Gemini CLI's context stream.
 */
export default async function afterToolCall(context) {
  if (!context || !context.tool || !context.result) return context;

  // We only care about tracking failures in active interactions
  if (context.tool.name === 'browser-act' || context.tool.name === 'browser-reason') {
    const output = JSON.stringify(context.result);
    if (output.includes('crash') || output.includes('"status":"error"') || output.includes('"type":"Error"')) {
      console.warn('\n⚠️ [BrowserNERD] Detected browser interaction error or crash.');
      console.warn('💡 Tip: Try using `/browser:look` to see the visual state, or call `browser-reason` with `topic="why_failed"`.\n');
      
      // If your hook wants to modify the result returned to the model, it can append instructions:
      if (typeof context.result === 'object' && context.result.content) {
        context.result.content.push({
          type: 'text',
          text: '\n[System injected via hook]: The last action resulted in a crash or error state. Consider using `browser-reason` to diagnose.'
        });
      }
    }
  }

  return context;
}
