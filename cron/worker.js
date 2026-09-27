// Cloudflare Worker: on each cron tick, start the GitHub sync workflow.
// GitHub's own schedule trigger is best-effort; a dispatch is not.
export default {
  async scheduled(_event, env, ctx) {
    ctx.waitUntil(dispatch(env));
  },
};

async function dispatch(env) {
  const url = `https://api.github.com/repos/${env.GITHUB_REPO}/actions/workflows/${env.WORKFLOW_FILE}/dispatches`;
  const res = await fetch(url, {
    method: "POST",
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${env.GITHUB_TOKEN}`,
      "X-GitHub-Api-Version": "2022-11-28",
      "User-Agent": "calendar-sync-cron",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ ref: "main" }),
  });
  if (res.status !== 204) {
    const body = await res.text();
    throw new Error(`dispatch failed: HTTP ${res.status} ${body}`);
  }
  console.log("dispatched sync workflow");
}
