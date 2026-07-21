// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

'use strict';

const fs = require('fs');

const MARKER_PREFIX = '<!-- meegle-cli-feishu-thread:';
const MARKER_SUFFIX = ' -->';
const DEFAULT_FEISHU_OPENAPI_BASE = 'https://open.feishu.cn';
const MAX_TEXT_LENGTH = 1800;
const MAX_POST_LINES = 20;

main().catch((error) => {
  console.error(error && error.stack ? error.stack : error);
  process.exit(1);
});

async function main() {
  const payload = readGitHubPayload();
  const eventName = process.env.GITHUB_EVENT_NAME || '';
  const config = readConfig();
  const github = createGitHubClient(config.githubToken);
  const feishu = createFeishuClient(config);

  if (eventName === 'workflow_dispatch') {
    await handleManualDispatch({ github, feishu, config });
    return;
  }

  const issue = payload.issue;
  if (!issue || issue.pull_request) {
    console.log('Ignored event without a regular issue payload.');
    return;
  }

  if (isInternalMarkerComment(payload)) {
    console.log('Ignored internal marker comment.');
    return;
  }

  const action = payload.action || 'unknown';
  const repo = getRepo();

  if (eventName === 'issues') {
    await handleIssueEvent({ github, feishu, config, repo, issue, payload, action });
    return;
  }

  if (eventName === 'issue_comment') {
    await handleIssueCommentEvent({ github, feishu, config, repo, issue, payload, action });
    return;
  }

  console.log(`Ignored unsupported event: ${eventName}`);
}

async function handleManualDispatch(options) {
  const { github, feishu, config } = options;
  const issueNumber = Number(process.env.MANUAL_ISSUE_NUMBER || 0);
  if (!issueNumber) {
    throw new Error('workflow_dispatch requires issue_number');
  }

  const repo = getRepo();
  const issue = await github.requestJson(`/repos/${repo}/issues/${issueNumber}`);
  const marker = await findFeishuMarker(github, repo, issue.number);
  const actor = process.env.GITHUB_ACTOR || 'github-actions';
  const activeMarker = marker || await createFeishuRootThread({
    github,
    feishu,
    config,
    repo,
    issue,
    action: 'synced',
    actor,
  });

  const rootRefreshed = await refreshRootMessage({
    github,
    feishu,
    config,
    repo,
    issue,
    marker: activeMarker,
    message: buildIssueRootMessage({
      repo,
      issue,
      action: 'synced',
      actor,
    }),
  });
  console.log(`${rootRefreshed ? 'Refreshed' : 'Skipped unchanged'} Feishu root thread: ${activeMarker.messageId}`);

  const refreshedCount = await syncExistingIssueComments({
    github,
    feishu,
    repo,
    issue,
    marker: activeMarker,
  });

  console.log(`Refreshed ${refreshedCount} issue comment message(s) for ${repo}#${issue.number}.`);
}

async function handleIssueEvent(options) {
  const { github, feishu, config, repo, issue, payload, action } = options;
  if (!shouldNotifyIssueAction(action)) {
    console.log(`Ignored issue action: ${action}`);
    return;
  }

  const marker = await findFeishuMarker(github, repo, issue.number);
  const actor = loginOf(payload.sender);

  if (!marker || !marker.messageId) {
    const sent = await feishu.sendMessage(config.feishuChatId, buildIssueRootMessage({
      repo,
      issue,
      action,
      actor,
    }));
    await createFeishuMarker(github, repo, issue.number, sent.message_id);
    console.log(`Created Feishu thread for ${repo}#${issue.number}: ${sent.message_id || '(unknown)'}`);
    return;
  }

  const refreshed = await refreshRootMessage({
    github,
    feishu,
    config,
    repo,
    issue,
    marker,
    message: buildIssueRootMessage({
      repo,
      issue,
      action,
      actor,
    }),
  });
  console.log(`${refreshed ? 'Refreshed' : 'Skipped unchanged'} Feishu thread for ${repo}#${issue.number}: ${marker.messageId}`);
}

async function handleIssueCommentEvent(options) {
  const { github, feishu, config, repo, issue, payload } = options;
  const marker = await findFeishuMarker(github, repo, issue.number);
  const actor = loginOf(payload.sender || payload.comment && payload.comment.user);

  if (!marker || !marker.messageId) {
    const sent = await feishu.sendMessage(config.feishuChatId, buildIssueRootMessage({
      repo,
      issue,
      action: 'commented',
      actor,
    }));
    const activeMarker = await createFeishuMarker(github, repo, issue.number, sent.message_id);
    const replied = await feishu.replyMessage(sent.message_id, buildIssueCommentMessage({
      repo,
      issue,
      comment: payload.comment,
      actor,
    }));
    await markCommentSynced(github, repo, activeMarker, payload.comment && payload.comment.id, replied.message_id);
    console.log(`Created Feishu thread from comment for ${repo}#${issue.number}: ${sent.message_id || '(unknown)'}`);
    console.log(`Replied with issue comment for ${repo}#${issue.number}: ${replied.message_id || '(unknown)'}`);
    return;
  }

  const commentMessage = buildIssueCommentMessage({
    repo,
    issue,
    comment: payload.comment,
    actor,
  });
  const existingMessageId = getCommentMessageId(marker, payload.comment && payload.comment.id);
  if (existingMessageId) {
    const refreshed = await refreshCommentMessage({
      github,
      feishu,
      repo,
      marker,
      commentId: payload.comment && payload.comment.id,
      message: commentMessage,
      existingMessageId,
    });
    console.log(`${refreshed ? 'Refreshed' : 'Skipped unchanged'} issue comment ${payload.comment.id} in Feishu`);
    return;
  }

  const replied = await feishu.replyMessage(marker.messageId, commentMessage);
  await markCommentSynced(github, repo, marker, payload.comment && payload.comment.id, replied.message_id);
  console.log(`Replied with issue comment for ${repo}#${issue.number}: ${replied.message_id || '(unknown)'}`);
}

function shouldNotifyIssueAction(action) {
  return [
    'opened',
    'reopened',
    'closed',
    'edited',
    'labeled',
    'unlabeled',
    'assigned',
    'unassigned',
  ].includes(action);
}

function buildIssueRootMessage(options) {
  const { repo, issue, action, actor } = options;
  const content = [
    [{ tag: 'text', text: `Issue ${action} by ${actor}` }],
    [{ tag: 'text', text: `State: ${issue.state || '-'}` }],
    [{ tag: 'a', text: 'Open issue', href: issue.html_url }],
  ];

  return buildPost(`[${repo}] #${issue.number} ${issue.title}`, content.concat(markdownToPostLines(
    issue.body || 'No description'
  )));
}

function buildIssueCommentMessage(options) {
  const { repo, issue, comment, actor } = options;
  const commentLines = markdownToPostLines(comment && comment.body ? comment.body : 'No comment body');
  const content = [
    [
      { tag: 'text', text: `Comment by ${actor}: ` },
      ...commentLines[0],
    ],
    ...commentLines.slice(1),
    [{ tag: 'a', text: 'Open comment', href: comment && comment.html_url ? comment.html_url : issue.html_url }],
  ];

  return buildPost(`[${repo}] #${issue.number} comment`, content);
}

function buildPost(title, content) {
  return {
    zh_cn: {
      title,
      content,
    },
  };
}

function buildCard(message) {
  const post = message && message.zh_cn ? message.zh_cn : { title: '', content: [] };
  const lines = post.content || [];
  const summary = postLineToMarkdown(lines[0] || []);
  const stateLine = findTextLine(lines, 'State: ');
  const bodyLines = lines.filter((line, index) => (
    index > 0 &&
    !isStateLine(line) &&
    !isLinkOnlyLine(line)
  )).map(postLineToMarkdown).filter(Boolean);
  const links = lines.flatMap((line) => (line || []).filter((element) => element && element.tag === 'a'));
  const elements = [];

  if (summary) {
    elements.push({
      tag: 'div',
      text: {
        tag: 'lark_md',
        content: `**${escapeLarkMarkdown(summary)}**`,
      },
    });
  }

  if (stateLine) {
    elements.push({
      tag: 'div',
      fields: [
        {
          is_short: true,
          text: {
            tag: 'lark_md',
            content: `**State**\n${escapeLarkMarkdown(stateLine.replace(/^State:\s*/, '') || '-')}`,
          },
        },
      ],
    });
  }

  if (bodyLines.length > 0) {
    elements.push({ tag: 'hr' });
    elements.push({
      tag: 'div',
      text: {
        tag: 'lark_md',
        content: trimText(bodyLines.join('\n'), MAX_TEXT_LENGTH),
      },
    });
  }

  if (links.length > 0) {
    elements.push({
      tag: 'action',
      actions: links.map((link) => ({
        tag: 'button',
        type: 'primary',
        text: {
          tag: 'plain_text',
          content: trimText(link.text || 'Open', 80),
        },
        url: link.href || '',
      })),
    });
  }

  return {
    config: {
      wide_screen_mode: true,
    },
    header: {
      template: cardHeaderTemplate(stateLine),
      title: {
        tag: 'plain_text',
        content: trimText(post.title || '', 200),
      },
    },
    elements,
  };
}

function findTextLine(lines, prefix) {
  for (const line of lines || []) {
    const text = textOnlyLine(line);
    if (text.startsWith(prefix)) {
      return text;
    }
  }
  return '';
}

function isStateLine(line) {
  return textOnlyLine(line).startsWith('State: ');
}

function isLinkOnlyLine(line) {
  return Boolean(
    line &&
    line.length === 1 &&
    line[0] &&
    line[0].tag === 'a'
  );
}

function textOnlyLine(line) {
  return (line || []).map((element) => (
    element && element.tag === 'text' ? element.text || '' : ''
  )).join('').trim();
}

function cardHeaderTemplate(stateLine) {
  const state = String(stateLine || '').toLowerCase();
  if (state.includes('closed')) {
    return 'grey';
  }
  if (state.includes('open')) {
    return 'green';
  }
  return 'blue';
}

function postLineToMarkdown(line) {
  return (line || []).map((element) => {
    if (!element || element.tag === 'text') {
      return escapeLarkMarkdown(element && element.text ? element.text : '');
    }
    if (element.tag === 'a') {
      return `[${escapeLarkMarkdown(element.text || element.href || 'link')}](${element.href || ''})`;
    }
    return '';
  }).join('').trim();
}

function escapeLarkMarkdown(value) {
  return String(value || '').replace(/([*_`~])/g, '\\$1');
}

async function findFeishuMarker(github, repo, issueNumber) {
  const comments = await github.listIssueComments(repo, issueNumber);
  for (const comment of comments) {
    const marker = parseMarker(comment.body || '');
    if (marker && marker.messageId) {
      return {
        commentId: comment.id,
        messageId: marker.messageId,
        syncedCommentIds: Array.isArray(marker.syncedCommentIds) ? marker.syncedCommentIds : [],
        commentMessages: normalizeCommentMessages(marker.commentMessages),
      };
    }
  }
  return null;
}

async function createFeishuMarker(github, repo, issueNumber, messageId) {
  if (!messageId) {
    throw new Error('Cannot create issue marker without Feishu message_id');
  }

  const marker = JSON.stringify({
    messageId,
    createdAt: new Date().toISOString(),
    syncedCommentIds: [],
    commentMessages: {},
  });

  const comment = await github.requestJson(`/repos/${repo}/issues/${issueNumber}/comments`, {
    method: 'POST',
    body: {
      body: `${MARKER_PREFIX}${marker}${MARKER_SUFFIX}\nSynced to Feishu topic.`,
    },
  });

  return {
    commentId: comment.id,
    messageId,
    syncedCommentIds: [],
    commentMessages: {},
  };
}

async function updateFeishuMarker(github, repo, marker) {
  const payload = JSON.stringify({
    messageId: marker.messageId,
    updatedAt: new Date().toISOString(),
    syncedCommentIds: marker.syncedCommentIds || [],
    commentMessages: normalizeCommentMessages(marker.commentMessages),
  });

  await github.requestJson(`/repos/${repo}/issues/comments/${marker.commentId}`, {
    method: 'PATCH',
    body: {
      body: `${MARKER_PREFIX}${payload}${MARKER_SUFFIX}\nSynced to Feishu topic.`,
    },
  });
}

async function markCommentSynced(github, repo, marker, commentId, commentMessageId) {
  if (!commentId || !marker || !marker.commentId) {
    return;
  }
  const changed = rememberCommentMessage(marker, commentId, commentMessageId);
  if (!changed) {
    return;
  }
  await updateFeishuMarker(github, repo, marker);
}

function rememberCommentMessage(marker, commentId, commentMessageId) {
  if (!commentId) {
    return false;
  }

  let changed = false;
  marker.syncedCommentIds = Array.isArray(marker.syncedCommentIds) ? marker.syncedCommentIds : [];
  if (!marker.syncedCommentIds.map(String).includes(String(commentId))) {
    marker.syncedCommentIds.push(commentId);
    changed = true;
  }

  if (commentMessageId) {
    marker.commentMessages = normalizeCommentMessages(marker.commentMessages);
    if (marker.commentMessages[String(commentId)] !== commentMessageId) {
      marker.commentMessages[String(commentId)] = commentMessageId;
      changed = true;
    }
  }

  return changed;
}

function getCommentMessageId(marker, commentId) {
  if (!marker || !commentId) {
    return '';
  }
  const commentMessages = normalizeCommentMessages(marker.commentMessages);
  return commentMessages[String(commentId)] || '';
}

function normalizeCommentMessages(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {};
  }

  const result = {};
  for (const [commentId, messageId] of Object.entries(value)) {
    if (commentId && typeof messageId === 'string' && messageId) {
      result[String(commentId)] = messageId;
    }
  }
  return result;
}

async function createFeishuRootThread(options) {
  const { github, feishu, config, repo, issue, action, actor } = options;
  const sent = await feishu.sendMessage(config.feishuChatId, buildIssueRootMessage({
    repo,
    issue,
    action,
    actor,
  }));
  const marker = await createFeishuMarker(github, repo, issue.number, sent.message_id);
  console.log(`Created Feishu thread for ${repo}#${issue.number}: ${sent.message_id || '(unknown)'}`);
  return marker;
}

async function refreshRootMessage(options) {
  const { github, feishu, config, repo, issue, marker, message } = options;
  try {
    return await feishu.updateMessageIfChanged(marker.messageId, message);
  } catch (error) {
    if (!isFeishuNotCardError(error)) {
      throw error;
    }

    const sent = await feishu.sendMessage(config.feishuChatId, message);
    marker.messageId = sent.message_id;
    marker.syncedCommentIds = [];
    marker.commentMessages = {};
    await updateFeishuMarker(github, repo, marker);
    console.log(`Migrated non-card Feishu root for ${repo}#${issue.number}: ${sent.message_id || '(unknown)'}`);
    return true;
  }
}

async function refreshCommentMessage(options) {
  const { github, feishu, repo, marker, commentId, message, existingMessageId } = options;
  try {
    return await feishu.updateMessageIfChanged(existingMessageId, message);
  } catch (error) {
    if (!isFeishuNotCardError(error)) {
      throw error;
    }

    const replied = await feishu.replyMessage(marker.messageId, message);
    await markCommentSynced(github, repo, marker, commentId, replied.message_id);
    console.log(`Migrated non-card Feishu comment ${commentId}: ${replied.message_id || '(unknown)'}`);
    return true;
  }
}

async function syncExistingIssueComments(options) {
  const { github, feishu, repo, issue, marker } = options;
  const comments = await github.listIssueComments(repo, issue.number);
  let refreshedCount = 0;
  let markerChanged = false;

  for (const comment of comments) {
    if (!comment || !comment.id || isMarkerBody(comment.body || '')) {
      continue;
    }

    const actor = loginOf(comment.user);
    const commentMessage = buildIssueCommentMessage({
      repo,
      issue,
      comment,
      actor,
    });
    const existingMessageId = getCommentMessageId(marker, comment.id);
    if (existingMessageId) {
      const refreshed = await refreshCommentMessage({
        github,
        feishu,
        repo,
        marker,
        commentId: comment.id,
        message: commentMessage,
        existingMessageId,
      });
      if (refreshed) {
        refreshedCount += 1;
      }
      console.log(`${refreshed ? 'Refreshed' : 'Skipped unchanged'} issue comment ${comment.id}: ${existingMessageId}`);
      continue;
    }

    const replied = await feishu.replyMessage(marker.messageId, commentMessage);
    markerChanged = rememberCommentMessage(marker, comment.id, replied.message_id) || markerChanged;
    refreshedCount += 1;
    console.log(`Backfilled issue comment ${comment.id}: ${replied.message_id || '(unknown)'}`);
  }

  if (markerChanged) {
    await updateFeishuMarker(github, repo, marker);
  }

  return refreshedCount;
}

function parseMarker(body) {
  const start = body.indexOf(MARKER_PREFIX);
  if (start < 0) {
    return null;
  }

  const jsonStart = start + MARKER_PREFIX.length;
  const end = body.indexOf(MARKER_SUFFIX, jsonStart);
  if (end < 0) {
    return null;
  }

  try {
    return JSON.parse(body.slice(jsonStart, end));
  } catch (error) {
    return null;
  }
}

function isInternalMarkerComment(payload) {
  return Boolean(
    payload &&
    payload.comment &&
    typeof payload.comment.body === 'string' &&
    isMarkerBody(payload.comment.body)
  );
}

function isMarkerBody(body) {
  return typeof body === 'string' && body.includes(MARKER_PREFIX);
}

function createFeishuClient(config) {
  let cachedToken = null;
  let cachedTokenExpiresAt = 0;

  async function getTenantAccessToken() {
    const now = Date.now();
    if (cachedToken && now < cachedTokenExpiresAt - 60 * 1000) {
      return cachedToken;
    }

    const data = await requestJson(`${config.feishuOpenapiBase}/open-apis/auth/v3/tenant_access_token/internal`, {
      method: 'POST',
      body: {
        app_id: config.feishuAppId,
        app_secret: config.feishuAppSecret,
      },
    });

    assertFeishuOK(data, 'get tenant_access_token');
    cachedToken = data.tenant_access_token;
    cachedTokenExpiresAt = now + Number(data.expire || 7200) * 1000;
    return cachedToken;
  }

  async function sendMessage(chatId, message) {
    const token = await getTenantAccessToken();
    const card = buildCard(message);
    const data = await requestJson(`${config.feishuOpenapiBase}/open-apis/im/v1/messages?receive_id_type=chat_id`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
      },
      body: {
        receive_id: chatId,
        msg_type: 'interactive',
        content: JSON.stringify(card),
      },
    });
    assertFeishuOK(data, 'send message');
    return data.data || {};
  }

  async function replyMessage(rootMessageId, message) {
    const token = await getTenantAccessToken();
    const card = buildCard(message);
    const data = await requestJson(`${config.feishuOpenapiBase}/open-apis/im/v1/messages/${encodeURIComponent(rootMessageId)}/reply`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
      },
      body: {
        msg_type: 'interactive',
        content: JSON.stringify(card),
      },
    });
    assertFeishuOK(data, 'reply message');
    return data.data || {};
  }

  async function getMessageContent(messageId) {
    const token = await getTenantAccessToken();
    const data = await requestJson(`${config.feishuOpenapiBase}/open-apis/im/v1/messages/${encodeURIComponent(messageId)}`, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });
    assertFeishuOK(data, 'get message');
    return data && data.data && data.data.body ? data.data.body.content : '';
  }

  async function updateMessage(messageId, message) {
    const token = await getTenantAccessToken();
    const card = buildCard(message);
    const data = await requestJson(`${config.feishuOpenapiBase}/open-apis/im/v1/messages/${encodeURIComponent(messageId)}`, {
      method: 'PATCH',
      headers: {
        Authorization: `Bearer ${token}`,
      },
      body: {
        content: JSON.stringify(card),
      },
    });
    assertFeishuOK(data, 'update message');
    return data.data || {};
  }

  async function updateMessageIfChanged(messageId, message) {
    const currentContent = await getMessageContent(messageId);
    const nextContent = JSON.stringify(buildCard(message));
    if (normalizeMessageContent(currentContent) === normalizeMessageContent(nextContent)) {
      return false;
    }

    await updateMessage(messageId, message);
    return true;
  }

  return {
    sendMessage,
    replyMessage,
    updateMessageIfChanged,
  };
}

function createGitHubClient(token) {
  const apiBase = process.env.GITHUB_API_URL || 'https://api.github.com';

  async function requestJson(path, options = {}) {
    const response = await fetch(`${apiBase}${path}`, {
      method: options.method || 'GET',
      headers: Object.assign({
        Accept: 'application/vnd.github+json',
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
        'X-GitHub-Api-Version': '2022-11-28',
      }, options.headers || {}),
      body: options.body ? JSON.stringify(options.body) : undefined,
    });

    const text = await response.text();
    const data = text ? JSON.parse(text) : null;
    if (!response.ok) {
      throw new Error(`GitHub API failed: ${response.status} ${text}`);
    }
    return data;
  }

  async function listIssueComments(repo, issueNumber) {
    const comments = [];
    let page = 1;

    while (page <= 10) {
      const chunk = await requestJson(`/repos/${repo}/issues/${issueNumber}/comments?per_page=100&page=${page}`);
      comments.push(...chunk);
      if (!Array.isArray(chunk) || chunk.length < 100) {
        break;
      }
      page += 1;
    }

    return comments;
  }

  return {
    requestJson,
    listIssueComments,
  };
}

async function requestJson(url, options = {}) {
  const response = await fetch(url, {
    method: options.method || 'GET',
    headers: Object.assign({
      Accept: 'application/json',
      'Content-Type': 'application/json; charset=utf-8',
    }, options.headers || {}),
    body: options.body ? JSON.stringify(options.body) : undefined,
  });

  const text = await response.text();
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    const error = new Error(`HTTP request failed: ${response.status} ${text}`);
    error.httpStatus = response.status;
    if (data && typeof data === 'object') {
      error.feishuCode = data.code;
      error.feishuMessage = data.msg || data.message;
    }
    throw error;
  }
  return data;
}

function assertFeishuOK(data, action) {
  if (!data || data.code !== 0) {
    const message = data && (data.msg || data.message) ? data.msg || data.message : 'unknown error';
    const error = new Error(`Feishu ${action} failed: ${message}`);
    if (data && typeof data === 'object') {
      error.feishuCode = data.code;
      error.feishuMessage = message;
    }
    throw error;
  }
}

function isFeishuNotCardError(error) {
  return Boolean(
    error &&
    (
      Number(error.feishuCode) === 230001 ||
      String(error.message || '').includes('This message is NOT a card')
    )
  );
}

function normalizeMessageContent(content) {
  if (typeof content !== 'string') {
    return stableStringify(content);
  }

  try {
    return stableStringify(JSON.parse(content));
  } catch (error) {
    return content;
  }
}

function stableStringify(value) {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(',')}]`;
  }
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort().map((key) => (
      `${JSON.stringify(key)}:${stableStringify(value[key])}`
    )).join(',')}}`;
  }
  return JSON.stringify(value);
}

function readGitHubPayload() {
  const eventPath = process.env.GITHUB_EVENT_PATH;
  if (!eventPath) {
    throw new Error('GITHUB_EVENT_PATH is missing');
  }
  return JSON.parse(fs.readFileSync(eventPath, 'utf8'));
}

function readConfig() {
  const missing = [];
  const config = {
    feishuAppId: process.env.FEISHU_APP_ID,
    feishuAppSecret: process.env.FEISHU_APP_SECRET,
    feishuChatId: process.env.FEISHU_CHAT_ID,
    feishuOpenapiBase: trimTrailingSlash(process.env.FEISHU_OPENAPI_BASE || DEFAULT_FEISHU_OPENAPI_BASE),
    githubToken: process.env.GITHUB_TOKEN,
  };

  for (const [key, value] of Object.entries({
    FEISHU_APP_ID: config.feishuAppId,
    FEISHU_APP_SECRET: config.feishuAppSecret,
    FEISHU_CHAT_ID: config.feishuChatId,
    GITHUB_TOKEN: config.githubToken,
  })) {
    if (!value) {
      missing.push(key);
    }
  }

  if (missing.length > 0) {
    throw new Error(`Missing required environment variables: ${missing.join(', ')}`);
  }

  return config;
}

function getRepo() {
  const repo = process.env.GITHUB_REPOSITORY;
  if (!repo) {
    throw new Error('GITHUB_REPOSITORY is missing');
  }
  return repo;
}

function trimText(value, maxLength) {
  const text = String(value || '');
  if (text.length <= maxLength) {
    return text;
  }
  return `${text.slice(0, maxLength - 20)}\n... truncated ...`;
}

function markdownToPostLines(value) {
  const text = trimText(String(value || ''), MAX_TEXT_LENGTH);
  const lines = text.split(/\r?\n/);
  const result = [];
  let inCodeBlock = false;

  for (const rawLine of lines) {
    if (result.length >= MAX_POST_LINES) {
      result.push([{ tag: 'text', text: '... truncated ...' }]);
      break;
    }

    const line = rawLine.replace(/\s+$/, '');
    if (line.trim().startsWith('```')) {
      inCodeBlock = !inCodeBlock;
      continue;
    }

    if (line.trim() === '') {
      if (result.length > 0 && result[result.length - 1].length > 0) {
        result.push([{ tag: 'text', text: ' ' }]);
      }
      continue;
    }

    result.push(markdownLineToPostElements(line, inCodeBlock));
  }

  return result.length > 0 ? result : [[{ tag: 'text', text: 'No content' }]];
}

function markdownLineToPostElements(line, inCodeBlock) {
  if (inCodeBlock) {
    return [{ tag: 'text', text: line }];
  }

  let text = line
    .replace(/^#{1,6}\s+/, '')
    .replace(/^>\s?/, '')
    .replace(/^[-*+]\s+/, '• ')
    .replace(/^(\d+)\.\s+/, '$1. ')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/__([^_]+)__/g, '$1')
    .replace(/`([^`]+)`/g, '$1');

  return inlineMarkdownToPostElements(text);
}

function inlineMarkdownToPostElements(text) {
  const elements = [];
  const pattern = /\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)|(https?:\/\/[^\s]+)/g;
  let lastIndex = 0;
  let match;

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      elements.push({ tag: 'text', text: text.slice(lastIndex, match.index) });
    }

    if (match[1] && match[2]) {
      elements.push({ tag: 'a', text: match[1], href: match[2] });
    } else if (match[3]) {
      elements.push({ tag: 'a', text: match[3], href: match[3] });
    }

    lastIndex = pattern.lastIndex;
  }

  if (lastIndex < text.length) {
    elements.push({ tag: 'text', text: text.slice(lastIndex) });
  }

  return elements.length > 0 ? elements : [{ tag: 'text', text }];
}

function trimTrailingSlash(value) {
  return String(value || '').replace(/\/+$/, '');
}

function loginOf(user) {
  return user && user.login ? user.login : 'unknown';
}
