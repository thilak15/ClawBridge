"""Granular Access Control List (ACL) policy engine for tool dispatch authorization."""

import fnmatch


class PolicyEngine:
    """Evaluates agent permission rules against target channels and tool actions."""

    @classmethod
    def is_authorized(cls, allowed_patterns: list[str], channel: str, action: str) -> bool:
        """Check whether a channel + action invocation is permitted by the agent's action patterns.

        Supported pattern formats:
        - `*`: global administrative permission across all channels and tools
        - `slack:*`: wildcard access to all actions within the 'slack' channel
        - `*:read*`: wildcard access to all read actions across all channels
        - `slack:post_message`: exact match permission
        """
        if not allowed_patterns:
            return False

        target = f"{channel.strip().lower()}:{action.strip().lower()}"

        for pattern in allowed_patterns:
            clean_pattern = pattern.strip().lower()
            if clean_pattern == "*":
                return True

            if fnmatch.fnmatchcase(target, clean_pattern):
                return True

        return False

