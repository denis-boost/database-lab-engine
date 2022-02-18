package estimator

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sample = `
TIME(s)     COMM           PID    DISK    T SECTOR     BYTES  LAT(ms)
0.000000    md2_raid1      342    nvme0n1 W 5244936    512       0.61
0.001041    md2_raid1      342    nvme1n1 W 5244936    512       0.61
0.001644    postgres       394    nvme1n1 W 35093536   520192    0.33
0.001658    postgres       394    nvme1n1 W 35094552   319488    0.29
0.001671    postgres       394    nvme0n1 W 35093536   520192    0.36
0.001719    postgres       394    nvme0n1 W 35094552   319488    0.36
0.004394    md2_raid1      342    nvme1n1 W 35095176   4096      0.62
0.004427    md2_raid1      342    nvme0n1 W 35095176   4096      0.66
0.381818    md2_raid1      342    nvme1n1 W 5244936    512       0.61
0.381830    md2_raid1      342    nvme0n1 W 5244936    512       0.62
0.390767    md2_raid1      342    nvme1n1 W 5244936    512       0.56
0.390778    md2_raid1      342    nvme0n1 W 5244936    512       0.57
0.390806    dockerd        899    nvme0n1 W 56763776   4096      0.01
0.390814    dockerd        899    nvme1n1 W 56763776   4096      0.02
0.390892    postgres       394    nvme0n1 W 35095184   53248     0.03
0.390900    postgres       394    nvme1n1 W 35095184   53248     0.03
0.392073    md2_raid1      342    nvme0n1 W 35095288   4096      0.52
0.392106    md2_raid1      342    nvme1n1 W 35095288   4096      0.55
0.392184    dockerd        899    nvme0n1 W 56579992   8192      0.01
0.392189    dockerd        899    nvme1n1 W 56579992   8192      0.01
0.392269    postgres       394    nvme1n1 W 35095296   36864     0.05
0.392274    postgres       394    nvme0n1 W 35095296   36864     0.05
0.395035    md2_raid1      342    nvme1n1 W 35095368   4096      0.58
0.395042    md2_raid1      342    nvme0n1 W 35095368   4096      0.59
0.645777    z_wr_iss       1261640 nvme1n1 W 1905510901 1024      0.71
0.645799    z_wr_iss       1261640 nvme0n1 W 1905510901 1024      0.74
0.645832    z_wr_int       741496 nvme1n1 W 1905510903 1024      0.01
0.645942    z_wr_int       741512 nvme0n1 W 166174565  16384     0.02
0.645777    z_wr_iss       1261636 nvme1n1 W 1902780362 512       0.71
0.645799    z_wr_iss       1261636 nvme0n1 W 1902780362 512       0.74
0.645844    z_wr_int       1261648 nvme0n1 W 1928235274 1024      0.01
0.645876    z_wr_int       1261648 nvme1n1 W 1929598000 1024      0.02
0.645898    z_wr_int       741492 nvme0n1 W 161257674  2048      0.01
0.645871    z_wr_int       741468 nvme0n1 W 161257662  1024      0.01
0.645847    z_wr_int       741480 nvme1n1 W 1928235274 1024      0.02
0.645878    z_wr_int       741480 nvme1n1 W 161257662  1024      0.02
0.645906    z_wr_int       1261643 nvme1n1 W 161257674  2048      0.02
0.645979    psql           1261645 nvme1n1 W 168889740  15360     0.06
0.646006    z_wr_int       1261644 nvme1n1 W 466853010  1024      0.01
0.646033    z_wr_int       741498 nvme1n1 W 688779565  1024      0.02
0.646030    z_wr_int       741516 nvme0n1 W 758462380  2048      0.01
0.646051    z_wr_int       741473 nvme0n1 W 799461576  1024      0.01
0.645861    z_wr_int       741508 nvme0n1 W 1929598000 1024      0.01
0.645982    z_wr_int       741478 nvme0n1 W 466853010  1024      0.01
0.646087    z_wr_int       741521 nvme0n1 W 1129587800 3072      0.02
0.645944    z_wr_int       741486 nvme1n1 W 166174565  16384     0.03
0.646011    postgres       1261642 nvme1n1 W 459634258  1536      0.05
0.646115    postgres       1261642 nvme1n1 W 688779577  1024      0.07
0.646012    z_wr_int       741490 nvme0n1 W 688779577  1024      0.01
`

func TestOutputScanner(t *testing.T) {
	testCases := []struct {
		pid       int
		readBytes uint64
	}{
		{
			pid:       394,
			readBytes: 1859584,
		},
		{
			pid:       1,
			readBytes: 0,
		},
		{
			pid:       1261645,
			readBytes: 15360,
		},
		{
			pid:       1261642,
			readBytes: 2560,
		},
	}

	for _, tc := range testCases {
		r := bytes.NewReader([]byte(sample))
		monitor := NewMonitor(tc.pid, "", &Profiler{})
		monitor.pidMapping = map[int]int{
			tc.pid: tc.pid,
		}

		monitor.scanOutput(context.TODO(), r)

		assert.Equal(t, tc.readBytes, monitor.profiler.readBytes)
	}
}

const procStatus = `
Name:   postgres            
Umask:  0077                                                                                                                                                                                               State:  S (sleeping)                                                                                 
Tgid:   2752157          
Ngid:   0                          
Pid:    2752157                   
PPid:   2747061
TracerPid:      0
Uid:    999     999     999     999
Gid:    999     999     999     999
FDSize: 64
Groups: 101 
NStgid: 2752157 674
NSpid:  2752157	674
NSpgid: 2752157 674
NSsid:  2752157 674
VmPeak:  2316996 kB
VmSize:  2315104 kB
`

func TestProcStatParsing(t *testing.T) {
	monitor := Monitor{}

	hostPID, err := monitor.parsePIDMapping([]byte(procStatus))

	require.Nil(t, err)
	assert.Equal(t, 674, hostPID)
}

const procCgroup = `
12:rdma:/
11:pids:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
10:cpuset:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
9:perf_event:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
8:blkio:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
7:freezer:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
6:cpu,cpuacct:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
5:memory:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
4:net_cls,net_prio:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
3:devices:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
2:hugetlb:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
1:name=systemd:/docker/ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b
`

func TestIsInside(t *testing.T) {
	containerHash := detectContainerHash([]byte(procCgroup))

	assert.Equal(t, "ad63ab82fdb32dd384ac76ab5a9d20fb7cb48f53be4d4cac52924e920c4a967b", containerHash)
}
