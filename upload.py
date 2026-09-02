#!/usr/bin/env python3
"""SFTP upload helper, tunneling through the HTTP CONNECT proxy."""
import socket, paramiko

HOST="frp.mini.gelsomino.cn"; PORT=50922; USER="root"; PASS="Zyz20050922!"
PROXY=("127.0.0.1",18080)

def tunnel():
    s=socket.create_connection(PROXY,timeout=30)
    s.sendall(f"CONNECT {HOST}:{PORT} HTTP/1.1\r\nHost: {HOST}:{PORT}\r\n\r\n".encode())
    r=b""
    while b"\r\n\r\n" not in r: r+=s.recv(4096)
    if b" 200" not in r.split(b"\r\n",1)[0]:
        raise ConnectionError(r)
    return s

c=paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST,port=PORT,username=USER,password=PASS,timeout=30,sock=tunnel(),allow_agent=False,look_for_keys=False)
sftp=c.open_sftp()
remote="/tmp/bocker-src.tar.gz"
sftp.put("/tmp/bocker-src.tar.gz", remote, confirm=False)
st=sftp.stat(remote)
print("uploaded:", remote, "size", st.st_size)
sftp.close(); c.close()