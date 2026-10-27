FROM postgres:18-bookworm
COPY deploy/maintenance.sh /usr/local/bin/picpilot-maintenance
RUN chmod 755 /usr/local/bin/picpilot-maintenance
ENTRYPOINT ["/usr/local/bin/picpilot-maintenance"]
CMD ["help"]
