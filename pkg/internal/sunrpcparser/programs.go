// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sunrpcparser

// Well-known RPC program numbers (rpcbind / ONC RPC).
const (
	ProgramPortmapper = 100000
	ProgramRstat      = 100001
	ProgramRusers     = 100002
	ProgramNFS        = 100003
	ProgramYpbind     = 100007
	ProgramMount      = 100005
	ProgramNFSACL     = 100227
	ProgramNlockmgr   = 100021
)

func ProgramName(prog uint32) string {
	switch prog {
	case ProgramPortmapper:
		return "portmapper"
	case ProgramRstat:
		return "rstat"
	case ProgramRusers:
		return "rusers"
	case ProgramNFS:
		return "nfs"
	case ProgramYpbind:
		return "ypbind"
	case ProgramMount:
		return "mount"
	case ProgramNFSACL:
		return "nfsacl"
	case ProgramNlockmgr:
		return "nlockmgr"
	default:
		return ""
	}
}

func procedureName(_ uint32, _ uint32) string {
	return ""
}

// AuthFlavorName returns a stable label for an RPC authentication flavor.
func AuthFlavorName(flavor uint32) string {
	switch flavor {
	case authNull:
		return "auth_null"
	case authUnix:
		return "auth_unix"
	case authShort:
		return "auth_short"
	case authDES:
		return "auth_des"
	case authRPCSECgss:
		return "rpcsec_gss"
	default:
		return ""
	}
}
