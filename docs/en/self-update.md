# In-panel updates

When a new Veilon release is published, a banner appears at the top of the panel. **Install update** shows the release notes and then:

1. downloads the new binary,
2. verifies its checksum,
3. backs up the program and the database,
4. replaces the program,
5. restarts the panel,
6. waits up to 30 seconds for a healthy start.

If the new version is not healthy, the previous binary is restored automatically and the panel keeps running. The panel is unreachable for a few seconds during the restart; keep the page open.

## Servers on v0.2.2 or older

In-panel updates need a small helper service that older installs do not have. Run this once on the server:

```bash
sudo veilon update
```

After that, every later update can be installed from the panel. `veilon update` on the server keeps working as before.

## Notes

- Rollback restores the program only. The database backup is kept on the server (its path is shown in the dialog) for a manual restore if ever needed.
