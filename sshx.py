#!/usr/bin/env python3
"""SSH helper for running commands on the remote server."""
import sys, paramiko

HOST = "frp.mini.gelsomino.cn"
PORT = 50922
USER = "root"
PASS = "Zyz20050922!"

PROXY = ("127.0.0.1", 18080)

def via_proxy():
    import socket
    sock = socket.create_connection(PROXY, timeout=30)
    sock.sendall(f"CONNECT {HOST}:{PORT} HTTP/1.1\r\nHost: {HOST}:{PORT}\r\n\r\n".encode())
    resp = b""
    while b"\r\n\r\n" not in resp:
        resp += sock.recv(4096)
    status = resp.split(b"\r\n", 1)[0]
    if b" 200" not in status:
        raise ConnectionError(f"CONNECT failed: {status}")
    return sock

def run(cmd, timeout=300):
    c = paramiko.SSHClient()
    c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    c.connect(HOST, port=PORT, username=USER, password=PASS, timeout=30, sock=via_proxy(), allow_agent=False, look_for_keys=False)
    stdin, stdout, stderr = c.exec_command(cmd, timeout=timeout)
    out = stdout.read().decode('utf-8', 'replace')
    err = stderr.read().decode('utf-8', 'replace')
    rc = stdout.channel.recv_exit_status()
    c.close()
    return rc, out, err

if __name__ == "__main__":
    cmd = " ".join(sys.argv[1:])
    rc, out, err = run(cmd)
    sys.stdout.write(out)
    if err:
        sys.stdout.write("\n[STDERR]\n" + err)
    sys.stdout.write(f"\n[EXIT={rc}]\n")