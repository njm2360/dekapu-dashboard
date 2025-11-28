import base64

def b64_urlsafe_decode(s: str) -> bytes:
    padding = 4 - (len(s) % 4)
    if padding != 4:
        s = s + ("=" * padding)
    return base64.urlsafe_b64decode(s)

def b64_urlsafe_encode(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()
