{
  config,
  lib,
  pkgs,
  ...
}:

with lib;

let
  cfg = config.services.qwdtt-cli;

  qwdtt-package = pkgs.buildGoModule {
    pname = "qwdtt-cli";
    version = "0.5.0";

    src = ./../..;
    vendorHash = null;

    subPackages = [ "." ];
    ldflags = [ "-s" "-w" ];

    postInstall = ''
      mkdir -p $out/share/bash-completion/completions
      cp $src/completions/qwdtt-cli.bash $out/share/bash-completion/completions/qwdtt-cli

      mkdir -p $out/share/fish/vendor_completions.d
      cp $src/completions/qwdtt-cli.fish $out/share/fish/vendor_completions.d/qwdtt-cli.fish
    '';

    meta = with lib; {
      description = "VPN client через TURN-серверы VK";
      license = licenses.mit;
      maintainers = [ ];
    };
  };
in
{
  options.services.qwdtt-cli = {
    enable = mkEnableOption "PWDTT CLI with capabilities";

    useVendor = mkOption {
      type = types.bool;
      default = true;
      description = ''
        Deprecated: vendor is always used now.
      '';
      visible = false;
    };

    deviceId = mkOption {
      type = types.nullOr (types.either types.str types.path);
      default = null;
      example = "0fd4ffcddb764351";
      description = ''
        Global Device ID (16 hex characters).
        Can be a string or a path to a file containing the ID.
        If not set, will be generated automatically on first run.
      '';
    };

    package = mkOption {
      type = types.package;
      default = qwdtt-package;
      defaultText = literalExpression "qwdtt-cli";
      description = ''
        The qwdtt-cli package to use.
      '';
    };

    wrappers = {
      enable = mkOption {
        type = types.bool;
        default = true;
        description = ''
          Whether to create security wrappers with capabilities for qwdtt-cli and ip.
          This allows running the tools without sudo.
        '';
      };

      group = mkOption {
        type = types.str;
        default = "users";
        description = ''
          Group that can execute the wrapped binaries.
        '';
      };
    };

    enableBashIntegration = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Whether to enable bash completion for qwdtt-cli.
        This will install the completion script to /etc/bash_completion.d/.
      '';
    };

    enableFishIntegration = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Whether to enable fish completion for qwdtt-cli.
        This will install the completion script to fish completions directory.
      '';
    };

    profiles = mkOption {
      type = types.attrsOf (types.submodule {
        options = {
          link = mkOption {
            type = types.either types.str types.path;
            description = ''
              wdtt:// link or path to a file containing the link.
              Example: "wdtt://1.2.3.4:56000:56001:9000:password:hash1,hash2#Name"
            '';
          };

          priority = mkOption {
            type = types.int;
            default = 0;
            description = ''
              Profile priority for auto-switch mode.
              Higher values are tried first.
            '';
          };

          deviceId = mkOption {
            type = types.nullOr (types.either types.str types.path);
            default = null;
            example = "0fd4ffcddb764351";
            description = ''
              Device ID for this specific profile (16 hex characters).
              Can be a string or a path to a file containing the ID.
              If not set, uses the global deviceId or generates automatically.
            '';
          };
        };
      });
      default = {};
      example = literalExpression ''
        {
          myserver = {
            link = "wdtt://1.2.3.4:56000:56001:9000:pass:hash#MyServer";
            priority = 100;
            deviceId = "0fd4ffcddb759420";
          };
          backup = {
            link = "/run/secrets/backup-server-link";
            priority = 50;
            deviceId = "/run/secrets/backup-device-id";
          };
        }
      '';
      description = ''
        Read-only profiles managed by NixOS configuration.
        Profile names will be prefixed with "ro-" (e.g., "myserver" becomes "ro-myserver").
        These profiles are read-only and can only be enabled/disabled via 'qwdtt-cli enable/disable' commands.
        Use regular 'add' command to create user-managed profiles instead.
      '';
    };

    users = mkOption {
      type = types.listOf types.str;
      default = [];
      example = [ "alice" "bob" ];
      description = ''
        List of users for whom read-only profiles should be created.
        Profiles will be created in each user's ~/.config/qwdtt/profiles/ directory.
        If empty, profiles are only created for root.
      '';
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.deviceId == null || (
          builtins.isPath cfg.deviceId ||
          (builtins.isString cfg.deviceId && (
            builtins.stringLength cfg.deviceId != 16 ||  # if not 16 chars, assume it's a path
            builtins.match "[0-9a-fA-F]{16}" cfg.deviceId != null  # if 16 chars, must be hex
          ))
        );
        message = "services.qwdtt-cli.deviceId (when string of 16 chars) must be hex characters (e.g., '0fd4ffcddb764351')";
      }
    ];

    environment.systemPackages = [
      cfg.package
      pkgs.wireguard-tools
      pkgs.iproute2
    ];

    system.activationScripts.qwdtt-device-id = mkIf (cfg.deviceId != null) (
      if builtins.isString cfg.deviceId && builtins.stringLength cfg.deviceId == 16
      then ''
        mkdir -p /root/.config/qwdtt
        echo -n "${cfg.deviceId}" > /root/.config/qwdtt/device_id
        chmod 600 /root/.config/qwdtt/device_id
      ''
      else ''
        mkdir -p /root/.config/qwdtt
        cat "${toString cfg.deviceId}" > /root/.config/qwdtt/device_id
        chmod 600 /root/.config/qwdtt/device_id
      ''
    );

    system.activationScripts.qwdtt-ro-profiles = mkIf (cfg.profiles != {}) ''
      # Create profiles for root
      mkdir -p /root/.config/qwdtt/ro-profiles

      ${concatStringsSep "\n" (mapAttrsToList (name: profile:
        let
          profileName = "ro-${name}";

          # Determine if link is a runtime path (sops secret) or static content
          isRuntimePath = builtins.isString profile.link &&
                         (builtins.match "^/run/secrets/.*" profile.link != null ||
                          builtins.match "^/etc/secrets/.*" profile.link != null);

          # Determine device ID path or value
          isDeviceIdRuntimePath = profile.deviceId != null && builtins.isString profile.deviceId &&
                                 (builtins.match "^/run/secrets/.*" profile.deviceId != null ||
                                  builtins.match "^/etc/secrets/.*" profile.deviceId != null);

          isGlobalDeviceIdRuntimePath = cfg.deviceId != null && builtins.isString cfg.deviceId &&
                                       (builtins.match "^/run/secrets/.*" cfg.deviceId != null ||
                                        builtins.match "^/etc/secrets/.*" cfg.deviceId != null);

          deviceIdValue = if isDeviceIdRuntimePath
                         then ""  # Will use device_id_file
                         else if profile.deviceId != null && builtins.isString profile.deviceId && builtins.stringLength profile.deviceId == 16
                         then profile.deviceId
                         else if profile.deviceId != null && builtins.isPath profile.deviceId
                         then builtins.replaceStrings ["\n" "\r"] ["" ""] (builtins.readFile profile.deviceId)
                         else if profile.deviceId != null && builtins.isString profile.deviceId && builtins.match "^/.*" profile.deviceId != null
                         then builtins.replaceStrings ["\n" "\r"] ["" ""] (builtins.readFile profile.deviceId)
                         else if isGlobalDeviceIdRuntimePath
                         then ""
                         else if cfg.deviceId != null && builtins.isString cfg.deviceId && builtins.stringLength cfg.deviceId == 16
                         then cfg.deviceId
                         else if cfg.deviceId != null && builtins.isPath cfg.deviceId
                         then builtins.replaceStrings ["\n" "\r"] ["" ""] (builtins.readFile cfg.deviceId)
                         else if cfg.deviceId != null && builtins.isString cfg.deviceId && builtins.match "^/.*" cfg.deviceId != null
                         then builtins.replaceStrings ["\n" "\r"] ["" ""] (builtins.readFile cfg.deviceId)
                         else "";

          deviceIdPath = if isDeviceIdRuntimePath
                        then profile.deviceId
                        else if isGlobalDeviceIdRuntimePath
                        then cfg.deviceId
                        else "";

        in
        # For runtime paths, store link_file; for static paths, parse and store values
        if isRuntimePath then ''
          # Create profile ${profileName} with link_file (runtime secret)
          mkdir -p /root/.config/qwdtt/ro-profiles
          cat > /root/.config/qwdtt/ro-profiles/${profileName}.json <<'NIXEOF'
{
  "listen": "127.0.0.1:9000",
  "link_file": "${profile.link}",
  ${if deviceIdValue != "" then ''"device_id": "${deviceIdValue}",'' else ""}
  "priority": ${toString profile.priority}
}
NIXEOF
          chmod 444 /root/.config/qwdtt/ro-profiles/${profileName}.json

          ${concatStringsSep "\n" (map (user: ''
            USER_HOME=$(getent passwd ${user} | cut -d: -f6)
            if [ -n "$USER_HOME" ] && [ -d "$USER_HOME" ]; then
              mkdir -p "$USER_HOME/.config/qwdtt/ro-profiles"
              cat > "$USER_HOME/.config/qwdtt/ro-profiles/${profileName}.json" <<'NIXEOF'
{
  "listen": "127.0.0.1:9000",
  "link_file": "${profile.link}",
  ${if deviceIdValue != "" then ''"device_id": "${deviceIdValue}",'' else ""}
  "priority": ${toString profile.priority}
}
NIXEOF
              chmod 444 "$USER_HOME/.config/qwdtt/ro-profiles/${profileName}.json"
              chown ${user}: "$USER_HOME/.config/qwdtt/ro-profiles/${profileName}.json"
              chown ${user}: "$USER_HOME/.config/qwdtt/ro-profiles" 2>/dev/null || true
              chown ${user}: "$USER_HOME/.config/qwdtt" 2>/dev/null || true
            fi
          '') cfg.users)}
        '' else
          let
            linkContent = if builtins.isPath profile.link
                         then builtins.readFile profile.link
                         else if builtins.isString profile.link && builtins.match "^/.*" profile.link != null
                         then builtins.readFile profile.link
                         else profile.link;

            stripped = builtins.replaceStrings ["wdtt://"] [""] linkContent;
            parts = builtins.split ":" stripped;
            ip = builtins.elemAt parts 0;
            dtlsPort = builtins.elemAt parts 2;

            tailParts = builtins.genList (i: builtins.elemAt parts (8 + i * 2))
                                         ((builtins.length parts - 8) / 2);
            tail = builtins.concatStringsSep ":" tailParts;
            passwordHashesFull = builtins.head (builtins.split "#" tail);
            passwordHashesParts = builtins.split ":" passwordHashesFull;
            passwordParts = builtins.filter builtins.isString passwordHashesParts;

            hashesList = if builtins.length passwordParts > 0
                        then builtins.elemAt passwordParts (builtins.length passwordParts - 1)
                        else "";
            password = if builtins.length passwordParts > 1
                      then builtins.concatStringsSep ":" (builtins.genList (i: builtins.elemAt passwordParts i) (builtins.length passwordParts - 1))
                      else "";

            hashesArray = if hashesList != ""
                         then builtins.fromJSON (builtins.toJSON (builtins.split "," hashesList))
                         else [];
            hashesFiltered = builtins.filter builtins.isString hashesArray;
            hashesJson = builtins.toJSON hashesFiltered;
          in ''
            # Create profile ${profileName} with parsed values (static content)
            mkdir -p /root/.config/qwdtt/ro-profiles
            cat > /root/.config/qwdtt/ro-profiles/${profileName}.json <<'NIXEOF'
{
  "peer": "${ip}:${dtlsPort}",
  "password": "${password}",
  "hashes": ${hashesJson},
  "listen": "127.0.0.1:9000",
  ${if deviceIdValue != "" then ''"device_id": "${deviceIdValue}",'' else ""}
  "priority": ${toString profile.priority}
}
NIXEOF
            chmod 444 /root/.config/qwdtt/ro-profiles/${profileName}.json

            ${concatStringsSep "\n" (map (user: ''
              USER_HOME=$(getent passwd ${user} | cut -d: -f6)
              if [ -n "$USER_HOME" ] && [ -d "$USER_HOME" ]; then
                mkdir -p "$USER_HOME/.config/qwdtt/ro-profiles"
                cat > "$USER_HOME/.config/qwdtt/ro-profiles/${profileName}.json" <<'NIXEOF'
{
  "peer": "${ip}:${dtlsPort}",
  "password": "${password}",
  "hashes": ${hashesJson},
  "listen": "127.0.0.1:9000",
  ${if deviceIdValue != "" then ''"device_id": "${deviceIdValue}",'' else ""}
  "priority": ${toString profile.priority}
}
NIXEOF
                chmod 444 "$USER_HOME/.config/qwdtt/ro-profiles/${profileName}.json"
                chown ${user}: "$USER_HOME/.config/qwdtt/ro-profiles/${profileName}.json"
                chown ${user}: "$USER_HOME/.config/qwdtt/ro-profiles" 2>/dev/null || true
                chown ${user}: "$USER_HOME/.config/qwdtt" 2>/dev/null || true
              fi
            '') cfg.users)}
          ''
      ) cfg.profiles)}
    '';

    security.wrappers = mkIf cfg.wrappers.enable {
      qwdtt-cli = {
        source = "${cfg.package}/bin/qwdtt-cli";
        capabilities = "cap_net_admin+eip";
        owner = "root";
        group = cfg.wrappers.group;
        permissions = "u+rx,g+x";
      };
      ip = {
        source = "${pkgs.iproute2}/bin/ip";
        capabilities = "cap_net_admin+eip";
        owner = "root";
        group = cfg.wrappers.group;
      };
    };

    boot.kernelModules = [ "wireguard" ];

    programs.bash.completion.enable = mkIf cfg.enableBashIntegration true;

    environment.pathsToLink = mkIf cfg.enableBashIntegration [
      "/share/bash-completion"
    ];

    programs.fish.enable = mkIf cfg.enableFishIntegration true;
  };
}
