"""Zenoh session and subscriber helper."""

from collections.abc import Callable
from typing import Any

import zenoh


class ZenohSubscriberHelper:
    """Manages a Zenoh session with one or more topic subscriptions."""

    def __init__(self, listen_endpoint: str = "tcp/127.0.0.1:7447"):
        conf = zenoh.Config.from_json5(f'{{"listen":{{"endpoints":["{listen_endpoint}"]}}}}')
        self._session = zenoh.open(conf)
        self._subs: list[Any] = []

    def subscribe(self, topic: str, callback: Callable[..., Any]) -> None:
        """Subscribe to a Zenoh topic with the given callback."""
        sub = self._session.declare_subscriber(topic, callback)
        self._subs.append(sub)

    def close(self) -> None:
        for s in self._subs:
            s.undeclare()
        self._session.close()
