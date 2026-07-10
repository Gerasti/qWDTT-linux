{
  config,
  lib,
  pkgs,
  ...
}:

with lib;

let
  cfg = config.services.qwdtt-cli;

  qwdtt-package = { useVendor ? true }: pkgs.buildGoModule {
    pname = "qwdtt-cli";
    version = "0.0.1";

    src = if useVendor then ./../.. else
      pkgs.lib.cleanSourceWith {
        src = ./../..;
        filter = path: type:
          let baseName = baseNameOf path;
          in baseName != "vendor";
      };

    vendorHash = if useVendor then null else "sha256-+F/FvtkwuocZc6N4RaSXC4jCgWeg8KTAPjsKnwMmjt8=";

    subPackages = [ "." ];
    ldflags = [ "-s" "-w" ];

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
        Use vendored dependencies from ./vendor directory.
        If set to false, dependencies will be fetched from network during build.
      '';
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
      default = qwdtt-package { useVendor = cfg.useVendor; };
      defaultText = literalExpression "qwdtt-cli built with useVendor setting";
      description = ''
        The qwdtt-cli package to use.
        By default, automatically builds based on useVendor option.
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
  };
}
