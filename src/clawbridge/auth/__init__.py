"""ClawBridge auth and access control module."""
from clawbridge.auth.tokens import TokenManager
from clawbridge.auth.registry import AgentRegistry, AgentRecord, AgentNotFoundError
from clawbridge.auth.acl import PolicyEngine
from clawbridge.auth.authenticator import (
    AgentAuthenticator,
    AuthenticationError,
    PermissionDeniedError,
    AgentContext,
)

__all__ = [
    "TokenManager",
    "AgentRegistry",
    "AgentRecord",
    "AgentNotFoundError",
    "PolicyEngine",
    "AgentAuthenticator",
    "AuthenticationError",
    "PermissionDeniedError",
    "AgentContext",
]
