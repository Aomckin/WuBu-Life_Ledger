ARG WHISPER_BASE_IMAGE=ghcr.io/ggerganov/whisper.cpp:main
FROM ${WHISPER_BASE_IMAGE}

COPY --chmod=755 whisper/entrypoint.sh /usr/local/bin/local-ai-whisper

ENTRYPOINT ["/usr/local/bin/local-ai-whisper"]
