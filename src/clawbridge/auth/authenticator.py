"""Agent authentication and ACL enforcement interceptor."""

from dataclasses import dataclass

from clawbridge.auth.acl import PolicyEngine
from clawbridge.auth.registry import AgentRegistry
from clawbridge.vault.vault import CredentialVault


class AuthenticationError(Exception):
    """Raised when an agent bearer token is missing, malformed, or invalid."""
    pass


class PermissionDeniedError(Exception):
    """Raised when an authenticated agent lacks ACL permissions for a channel/action."""
    pass


@dataclass(frozen=True)
class AgentContext:
    """Represents an authenticated and authorized agent execution context."""
    agent_id: str
    agent_name: str
    channel: str
    action: str


class AgentAuthenticator:
    """Enforces zero-trust agent identity validation and per-tool granular ACL dispatch."""

    def __init__(self, registry: AgentRegistry, vault: CredentialVault):
        self.registry = registry
        self.vault = vault

    def authenticate_and_authorize(
        self,
        bearer_header: str | None,
        channel: str,
        action: str,
    ) -> AgentContext:
        """Validate agent bearer token and enforce ACL authorization before tool execution."""
        if not bearer_header or not bearer_header.startswith("Bearer "):
            self.vault.record_audit_event(
                event_type="auth_failure",
                status="rejected",
                channel=channel,
                details={"reason": "Missing or malformed Authorization header", "action": action},
            )
            raise AuthenticationError("Missing or malformed Authorization header (expected 'Bearer <token>')")

        raw_token = bearer_header[len("Bearer "):].strip()
        agent = self.registry.get_agent_by_token(raw_token)
        if not agent:
            self.vault.record_audit_event(
                event_type="auth_failure",
                status="rejected",
                channel=channel,
                details={"reason": "Invalid or revoked agent token", "action": action},
            )
            raise AuthenticationError("Invalid or revoked agent token")

        # Evaluate Granular ACL Policy
        if not PolicyEngine.is_authorized(agent.allowed_actions, channel, action):
            self.vault.record_audit_event(
                event_type="permission_denied",
                status="forbidden",
                agent_id=agent.id,
                channel=channel,
                details={
                    "action": action,
                    "allowed_actions": agent.allowed_actions,
                    "denied_reason": "ACL policy restriction",
                },
            )
            raise PermissionDeniedError(
                f"Agent '{agent.id}' ({agent.name}) is not authorized to invoke '{channel}:{action}'"
            )

        # Record successful authorization
        self.vault.record_audit_event(
            event_type="auth_success",
            status="authorized",
            agent_id=agent.id,
            channel=channel,
            details={"action": action},
        )

        return AgentContext(
            agent_id=agent.id,
            agent_name=agent.name,
            channel=channel,
            action=action,
        )
