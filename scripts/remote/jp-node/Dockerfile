FROM autumn27/scopesentry-scan@sha256:55f803d76641e8a5143ac386360fb6c9602710492170a8fc50b0326dfb29fc04
COPY --chmod=0755 ScopeSentry-Scan /apps/ScopeSentry-Scan
COPY --chmod=0755 healthcheck.py /usr/local/libexec/scopesentry-healthcheck.py
LABEL org.scopesentry.node.version="1.9.20" org.scopesentry.binary.sha256="ec7eaa34bf52834768c198f5f823b54eab9ede1d769ab39e872365ee0b81cef1"
WORKDIR /apps
ENTRYPOINT ["/apps/ScopeSentry-Scan"]
