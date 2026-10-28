"""ClawBridge Model Context Protocol (MCP) server tool handlers."""

import dataclasses
from typing import Any, Mapping
from clawbridge.dispatch.dispatcher import EgressDispatcher


class ClawBridgeTools:
    """Provides high-level normalized tool operations mapped to downstream services via EgressDispatcher."""

    def __init__(
        self,
        dispatcher: EgressDispatcher,
        default_token_ids: Mapping[str, str] | None = None,
        default_credential_ids: Mapping[str, str] | None = None,
    ):
        self.dispatcher = dispatcher
        self.default_token_ids = dict(default_token_ids or {})
        self.default_credential_ids = dict(default_credential_ids or {})

    def _format_bearer(self, token: str) -> str:
        if not token.startswith("Bearer "):
            return f"Bearer {token}"
        return token

    async def slack_post_message(
        self,
        agent_token: str,
        channel_id: str,
        text: str,
        token_id: str | None = None,
    ) -> dict[str, Any]:
        """Post a chat message to a Slack channel."""
        resolved_token_id = token_id or self.default_token_ids.get("slack")
        receipt = await self.dispatcher.dispatch(
            bearer_header=self._format_bearer(agent_token),
            channel="slack",
            action="post_message",
            method="POST",
            url="https://slack.com/api/chat.postMessage",
            token_id=resolved_token_id,
            json={"channel": channel_id, "text": text},
        )
        return dataclasses.asdict(receipt)

    async def jira_create_issue(
        self,
        agent_token: str,
        project_key: str,
        summary: str,
        description: str = "",
        issue_type: str = "Task",
        credential_id: str | None = None,
        jira_base_url: str = "https://your-domain.atlassian.net",
    ) -> dict[str, Any]:
        """Create a Jira issue in a given project."""
        resolved_cred_id = credential_id or self.default_credential_ids.get("jira")
        url = f"{jira_base_url.rstrip('/')}/rest/api/3/issue"
        receipt = await self.dispatcher.dispatch(
            bearer_header=self._format_bearer(agent_token),
            channel="jira",
            action="create_issue",
            method="POST",
            url=url,
            credential_id=resolved_cred_id,
            json={
                "fields": {
                    "project": {"key": project_key},
                    "summary": summary,
                    "description": {
                        "type": "doc",
                        "version": 1,
                        "content": [
                            {
                                "type": "paragraph",
                                "content": [{"type": "text", "text": description}],
                            }
                        ],
                    },
                    "issuetype": {"name": issue_type},
                }
            },
        )
        return dataclasses.asdict(receipt)

    async def twitter_post_tweet(
        self,
        agent_token: str,
        text: str,
        token_id: str | None = None,
    ) -> dict[str, Any]:
        """Post a tweet (X post) on behalf of an authenticated account."""
        resolved_token_id = token_id or self.default_token_ids.get("twitter")
        receipt = await self.dispatcher.dispatch(
            bearer_header=self._format_bearer(agent_token),
            channel="twitter",
            action="post_tweet",
            method="POST",
            url="https://api.twitter.com/2/tweets",
            token_id=resolved_token_id,
            json={"text": text},
        )
        return dataclasses.asdict(receipt)

    async def discord_send_message(
        self,
        agent_token: str,
        channel_id: str,
        content: str,
        credential_id: str | None = None,
    ) -> dict[str, Any]:
        """Send a message to a Discord channel."""
        resolved_cred_id = credential_id or self.default_credential_ids.get("discord")
        url = f"https://discord.com/api/v10/channels/{channel_id}/messages"
        receipt = await self.dispatcher.dispatch(
            bearer_header=self._format_bearer(agent_token),
            channel="discord",
            action="send_message",
            method="POST",
            url=url,
            credential_id=resolved_cred_id,
            json={"content": content},
        )
        return dataclasses.asdict(receipt)
