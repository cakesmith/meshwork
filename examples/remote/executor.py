"""Independent Python reference peer for meshwork.executor.v1 (standard library only)."""
import argparse
import hmac
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse
from urllib.request import urlopen

PROTOCOL = "meshwork.executor.v1"
LIMIT = 4 * 1024 * 1024


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=8081)
    args = parser.parse_args()
    secret = os.environ["MESHWORK_EXECUTOR_TOKEN"]
    if not secret:
        raise ValueError("MESHWORK_EXECUTOR_TOKEN must not be empty")

    class Handler(BaseHTTPRequestHandler):
        def authorized(self):
            if not hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + secret):
                self.send_error(401)
                return False
            return True

        def reply(self, value):
            body = json.dumps(value, allow_nan=False).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if not self.authorized():
                return
            if self.path != "/capabilities":
                self.send_error(404)
                return
            self.reply({"protocol": PROTOCOL, "transport": "http-json", "implementations": ["logic.nand.go"]})

        def do_POST(self):
            if not self.authorized():
                return
            if self.path != "/execute":
                self.send_error(404)
                return
            result = {"protocol": PROTOCOL, "id": ""}
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if not 0 < length <= LIMIT:
                    raise ValueError("invalid request size")
                request = json.loads(self.rfile.read(length))
                result["id"] = request["id"]
                if request["protocol"] != PROTOCOL or request["implementation"] != "logic.nand.go":
                    raise ValueError("unsupported invocation")
                inputs = {}
                for port, endpoint in request["inputs"].items():
                    parsed = urlparse(endpoint)
                    if parsed.scheme not in ("http", "https") or not parsed.path.startswith("/inputs/"):
                        raise ValueError("invalid input capability")
                    with urlopen(endpoint, timeout=10) as response:
                        data = response.read(LIMIT + 1)
                    if len(data) > LIMIT:
                        raise ValueError("input too large")
                    token = json.loads(data)
                    if token["type"] != "Bool" or type(token["value"]) is not bool:
                        raise ValueError("expected Bool token")
                    inputs[port] = token["value"]
                if set(inputs) != {"A", "B"}:
                    raise ValueError("expected A and B input ports")
                result["outputs"] = {"Q": {"type": "Bool", "value": not (inputs["A"] and inputs["B"])}}
            except Exception:
                # Do not put input capability URLs into error responses or logs.
                result["error"] = "invalid invocation or input retrieval failed"
            self.reply(result)

        def log_message(self, *_):
            pass

    ThreadingHTTPServer(("127.0.0.1", args.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
