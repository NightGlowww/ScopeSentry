FROM autumn27/scopesentry-scan@sha256:55f803d76641e8a5143ac386360fb6c9602710492170a8fc50b0326dfb29fc04
COPY --chmod=0755 ScopeSentry-Scan /apps/ScopeSentry-Scan
COPY --chmod=0755 healthcheck.py /usr/local/libexec/scopesentry-healthcheck.py
# nuclei 每个批次都会读 .nuclei-ignore；文件缺失或为空都会打一行 ERR（一次扫描上千批 = 上千行）。
# v3.3.6 的格式是 YAML：{tags: [], files: []}，空文件会报 EOF、纯文本会报 unmarshal。
RUN mkdir -p /root/.config/nuclei && printf "tags: []\nfiles: []\n" > /root/.config/nuclei/.nuclei-ignore
LABEL org.scopesentry.node.version="1.9.20" org.scopesentry.binary.sha256="7a84e2d20fa6222784728551e80f778d23e27fdf75b65bfc3262807dd1a4f38a"
WORKDIR /apps
ENTRYPOINT ["/apps/ScopeSentry-Scan"]
