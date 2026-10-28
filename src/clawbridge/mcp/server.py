"""Model Context Protocol (MCP) server creation and tool registration."""

from typing import Any
from mcp.server.mcpserver import MCPServer
from clawbridge.mcp.tools import ClawBridgeTools


def create_mcp_server(tools: ClawBridgeTools, name: str = "ClawBridge") -> MCPServer:
    """Factory creating an MCPServer instance configured with normalized ClawBridge tools."""
    server = MCPServer(name)

    @server.tool(
        name="slack_post_message",
        description="Post a chat message to a Slack channel using an authenticated agent bearer token.",
    )
    async def slack_post_message(
        agent_token: str,
        channel_id: str,
        text: str,
        token_id: str | None = None,
    ) -> dict[str, Any]:
        return await tools.slack_post_message(
            agent_token=agent_token,
            channel_id=channel_id,
            text=text,
            token_id=token_id,
        )

    @server.tool(
        name="jira_create_issue",
        description="Create a Jira issue in a given project using an authenticated agent bearer token.",
    )
    async def jira_create_issue(
        agent_token: str,
        project_key: str,
        summary: str,
        description: str = "",
        issue_type: str = "Task",
        credential_id: str | None = None,
        jira_base_url: str = "https://your-domain.atlassian.net",
    ) -> dict[str, Any]:
        return await tools.jira_create_issue(
            agent_token=agent_token,
            project_key=project_key,
            summary=summary,
            description=description,
            issue_type=issue_type,
            credential_id=credential_id,
            jira_base_url=jira_base_url,
        )

    @server.tool(
        name="twitter_post_tweet",
        description="Post a tweet (X post) on behalf of an authenticated account using an agent bearer token.",
    )
    async def twitter_post_tweet(
        agent_token: str,
        text: str,
        token_id: str | None = None,
    ) -> dict[str, Any]:
        return await tools.twitter_post_tweet(
            agent_token=agent_token,
            text=text,
            token_id=token_id,
        )

    @server.tool(
        name="discord_send_message",
        description="Send a message to a Discord channel using an authenticated agent bearer token.",
    )
    async def discord_send_message(
        agent_token: str,
        channel_id: str,
        content: str,
        credential_id: str | None = None,
    ) -> dict[str, Any]:
        return await tools.discord_send_message(
            agent_token=agent_token,
            channel_id=channel_id,
            content=content,
            credential_id=credential_id,
        )

    return server
