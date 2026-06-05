package agent

// QueuedInputReminder is inserted as a standalone user message before queued
// inputs when the agent is in the middle of a tool-call loop. It nudges the
// LLM to decide whether to interrupt its current task or continue.
const QueuedInputReminder = `<system-reminder>
用户在你执行工具调用期间发送了新消息。请自行判断：是中断当前任务立即处理用户的新请求，还是完成当前任务后再处理。
</system-reminder>`
