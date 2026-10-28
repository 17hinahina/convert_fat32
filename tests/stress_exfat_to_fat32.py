#!/usr/bin/env python3
import argparse
import hashlib
import json
import math
import os
import random
import struct
import subprocess
import sys
import time
from pathlib import Path

RS = 32
NF = 2
ROOT_CLUSTER = 2
MIN_FAT32_CLUSTERS = 65525
MAX_FAT32_CLUSTERS = 0x0FFFFFF5
CAPACITIES = {
    "256GB": 256_000_000_000,
    "512GB": 512_000_000_000,
    "1TB": 1_000_000_000_000,
    "2TB": 2_000_000_000_000,
}
BPS_CHOICES = (512, 1024, 2048, 4096)
CLUSTER_CHOICES = (0, 4096, 8192, 16384, 32768, 65536)


def p16(b, o, v): struct.pack_into('<H', b, o, v)
def p32(b, o, v): struct.pack_into('<I', b, o, v)
def p64(b, o, v): struct.pack_into('<Q', b, o, v)
def u16(b, o): return struct.unpack_from('<H', b, o)[0]
def u32(b, o): return struct.unpack_from('<I', b, o)[0]
def u64(b, o): return struct.unpack_from('<Q', b, o)[0]

def is_power_of_two(v): return v != 0 and (v & (v - 1)) == 0

def auto_cluster_bytes(nbytes, bps):
    c = 32 * 1024
    gib = 1024 ** 3
    if nbytes <= 8 * gib: c = 4 * 1024
    elif nbytes <= 16 * gib: c = 8 * 1024
    elif nbytes <= 32 * gib: c = 16 * 1024
    if c < bps: c = bps
    return c

def compute_fat_sectors(total_sectors, bps, spc):
    # Exact port of v0.2.1 computeFatSectors.
    fat_sectors = 1
    for _ in range(64):
        used = RS + NF * fat_sectors
        if used >= total_sectors:
            raise ValueError("volume is too small")
        data_sectors = total_sectors - used
        clusters = data_sectors // spc
        fat_bytes = (clusters + 2) * 4
        required = (fat_bytes + bps - 1) // bps
        if required <= fat_sectors:
            while (RS + NF * fat_sectors) % spc != 0:
                fat_sectors += 1
            aligned_used = RS + NF * fat_sectors
            if aligned_used >= total_sectors:
                raise ValueError("invalid FAT layout")
            cluster_count = (total_sectors - aligned_used) // spc
            capacity_entries = fat_sectors * bps // 4
            if capacity_entries < cluster_count + 2:
                raise ValueError("FAT is too small for computed cluster count")
            return fat_sectors, cluster_count
        fat_sectors = required
    raise ValueError("could not converge FAT size")

def sanitize_label(s):
    s = s.strip().upper()
    out=[]
    for ch in s:
        if len(out) >= 11: break
        if ('A' <= ch <= 'Z') or ('0' <= ch <= '9') or ch in ' _-': out.append(ch)
    return ''.join(out).rstrip()

def label11(label):
    label = sanitize_label(label) or 'NO NAME'
    return label.encode('ascii').ljust(11, b' ')[:11]

def fat32_layout(volume_bytes, bps, cluster_bytes):
    total64 = volume_bytes // bps
    if total64 == 0 or total64 > 0xffffffff:
        raise ValueError('volume too large for FAT32 BPB')
    total = total64
    if cluster_bytes == 0:
        cluster_bytes = auto_cluster_bytes(volume_bytes, bps)
    if (not is_power_of_two(cluster_bytes) or cluster_bytes < bps or cluster_bytes > 65536 or cluster_bytes % bps):
        raise ValueError('invalid cluster size')
    spc = cluster_bytes // bps
    if not is_power_of_two(spc) or spc == 0 or spc > 128:
        raise ValueError('invalid sectors-per-cluster')
    fat, cc = compute_fat_sectors(total, bps, spc)
    if cc < MIN_FAT32_CLUSTERS:
        raise ValueError('too few clusters')
    if cc > MAX_FAT32_CLUSTERS:
        raise ValueError('too many clusters')
    fat_bytes = fat*bps
    fat1 = RS*bps
    fat2 = fat1+fat_bytes
    data = fat2+fat_bytes
    return dict(volume_bytes=volume_bytes,bps=bps,total=total,cluster_bytes=cluster_bytes,spc=spc,
                fat=fat,clusters=cc,fat_bytes=fat_bytes,fat1=fat1,fat2=fat2,data=data,root=data)

# exFAT source image helpers based on Microsoft exFAT specification.
def ror1(v): return ((v >> 1) | ((v & 1) << 31)) & 0xffffffff

def exfat_boot_checksum(region, bps):
    c=0
    for i, x in enumerate(region[:11*bps]):
        if i in (106,107,112):
            continue
        c=(ror1(c)+x)&0xffffffff
    return c

def build_exfat_boot_region(volume_bytes, bps, serial, percent, partition_offset, exfat_cluster_bytes):
    total = volume_bytes // bps
    spc=exfat_cluster_bytes//bps
    if not is_power_of_two(spc): raise ValueError('bad exfat spc')
    fat_off=max(24, (1024*1024)//bps)
    fat_len=1
    for _ in range(64):
        heap=((fat_off+fat_len+spc-1)//spc)*spc
        clusters=(total-heap)//spc
        req=((clusters+2)*4+bps-1)//bps
        if req <= fat_len: break
        fat_len=req
    heap=((fat_off+fat_len+spc-1)//spc)*spc
    clusters=(total-heap)//spc
    if clusters <= 0 or clusters > 0xfffffff5: raise ValueError('bad exfat cluster count')
    region=bytearray(24*bps)
    for base in (0,12):
        off=base*bps
        bs=memoryview(region)[off:off+bps]
        bs[0:3]=b'\xEB\x76\x90'
        bs[3:11]=b'EXFAT   '
        p64(bs,64,partition_offset)
        p64(bs,72,total)
        p32(bs,80,fat_off)
        p32(bs,84,fat_len)
        p32(bs,88,heap)
        p32(bs,92,clusters)
        p32(bs,96,2)
        p32(bs,100,serial)
        p16(bs,104,0x0100)
        p16(bs,106,0)
        bs[108]=int(math.log2(bps))
        bs[109]=int(math.log2(spc))
        bs[110]=1
        bs[111]=0x80
        bs[112]=percent
        bs[120:510]=b'\xF4'*(510-120)
        bs[510:512]=b'\x55\xAA'
        for s in range(base+1,base+9):
            p32(region, s*bps+bps-4, 0xAA550000)
        c=exfat_boot_checksum(region[base*bps:(base+12)*bps],bps)
        for pos in range((base+11)*bps,(base+12)*bps,4):
            p32(region,pos,c)
    return region

def validate_exfat_boot(region, volume_bytes, bps):
    assert region[3:11] == b'EXFAT   '
    assert region[12*bps+3:12*bps+11] == b'EXFAT   '
    assert region[510:512] == b'\x55\xAA'
    assert u64(region,72) == volume_bytes//bps
    assert region[108] == int(math.log2(bps))
    for base in (0,12):
        off=base*bps
        expected=u32(region,(base+11)*bps)
        actual=exfat_boot_checksum(region[off:(base+12)*bps],bps)
        assert actual == expected
        for pos in range((base+11)*bps,(base+12)*bps,4): assert u32(region,pos)==expected


def make_fat32_metadata(layout, hidden, volume_id, label):
    bps=layout['bps']; spc=layout['spc']; total=layout['total']; fat=layout['fat']; cc=layout['clusters']
    boot=bytearray(bps)
    boot[0:3]=b'\xEB\x58\x90'; boot[3:11]=b'MSWIN4.1'
    p16(boot,11,bps); boot[13]=spc; p16(boot,14,RS); boot[16]=NF
    p16(boot,17,0); p16(boot,19,0); boot[21]=0xF8; p16(boot,22,0)
    p16(boot,24,63); p16(boot,26,255); p32(boot,28,hidden); p32(boot,32,total)
    p32(boot,36,fat); p16(boot,40,0); p16(boot,42,0); p32(boot,44,ROOT_CLUSTER); p16(boot,48,1); p16(boot,50,6)
    boot[64]=0x80; boot[66]=0x29; p32(boot,67,volume_id)
    l11=label11(label); boot[71:82]=l11; boot[82:90]=b'FAT32   '; boot[90:107]=b'FAT32 data volume'
    boot[510:512]=b'\x55\xAA'
    fs=bytearray(bps); p32(fs,0,0x41615252); p32(fs,484,0x61417272); p32(fs,488,cc-1); p32(fs,492,3); p32(fs,508,0xAA550000)
    fat0=bytearray(bps); p32(fat0,0,0x0FFFFFF8); p32(fat0,4,0x0FFFFFFF); p32(fat0,8,0x0FFFFFFF)
    root=bytearray(layout['cluster_bytes'])
    cleaned=sanitize_label(label)
    if cleaned:
        root[:11]=l11; root[11]=0x08
    return boot,fs,fat0,root


def validate_fat32(layout, boot, fs, fat0, root, hidden, label):
    bps=layout['bps']
    assert boot[0:3]==b'\xEB\x58\x90' and boot[3:11]==b'MSWIN4.1'
    assert u16(boot,11)==bps and boot[13]==layout['spc'] and u16(boot,14)==RS and boot[16]==NF
    assert u32(boot,28)==hidden and u32(boot,32)==layout['total'] and u32(boot,36)==layout['fat']
    assert u32(boot,44)==2 and u16(boot,48)==1 and u16(boot,50)==6 and boot[82:90]==b'FAT32   '
    assert boot[510:512]==b'\x55\xAA'
    assert u32(fs,0)==0x41615252 and u32(fs,484)==0x61417272 and u32(fs,488)==layout['clusters']-1 and u32(fs,492)==3 and u32(fs,508)==0xAA550000
    assert u32(fat0,0)==0x0FFFFFF8 and u32(fat0,4)==0x0FFFFFFF and u32(fat0,8)==0x0FFFFFFF
    assert layout['fat']*bps//4 >= layout['clusters']+2
    assert layout['data'] % layout['cluster_bytes'] == 0
    assert layout['data'] + layout['clusters']*layout['cluster_bytes'] <= layout['volume_bytes']
    assert layout['volume_bytes'] - (layout['data'] + layout['clusters']*layout['cluster_bytes']) < layout['cluster_bytes']
    cleaned=sanitize_label(label)
    if cleaned:
        assert root[:11]==label11(label) and root[11]==0x08
    # No exFAT signature survives in FAT32 boot metadata.
    assert b'EXFAT   ' not in boot
    assert b'EXFAT   ' not in fs


def stress_one(name, size, loops, seed):
    rng=random.Random(seed)
    valid_clusters={}
    # Prebuild and fully validate representative exFAT templates. Per iteration we
    # mutate only VolumeFlags/PercentInUse, which are explicitly excluded from
    # exFAT BootChecksum, so the source remains checksum-valid without O(sectors)
    # checksum work in the hot loop.
    ex_templates={}
    layout_cache={}
    for bps in BPS_CHOICES:
        vol=size-size%bps
        vals=[]
        for cb in CLUSTER_CHOICES:
            try:
                lay=fat32_layout(vol,bps,cb)
                vals.append(cb)
                layout_cache[(bps,cb)]=lay
            except ValueError:
                pass
        valid_clusters[bps]=vals
        ex=build_exfat_boot_region(vol,bps,0x13579BDF,0,2048,128*1024)
        validate_exfat_boot(ex,vol,bps)
        ex_templates[bps]=ex
    started=time.perf_counter()
    digest=hashlib.sha256()
    for i in range(loops):
        bps=BPS_CHOICES[rng.randrange(len(BPS_CHOICES))]
        vol=size-size%bps
        cands=valid_clusters[bps]
        cb=cands[rng.randrange(len(cands))]
        # Re-run the actual v0.2.1 layout calculation every iteration, then
        # compare against the precomputed expected result.
        layout=fat32_layout(vol,bps,cb)
        exp=layout_cache[(bps,cb)]
        assert layout['fat']==exp['fat'] and layout['clusters']==exp['clusters'] and layout['data']==exp['data']

        ex=ex_templates[bps]
        # Source state: valid exFAT signature; VolumeFlags and PercentInUse are
        # checksum-exempt per the exFAT specification and can vary in live media.
        assert ex[3:11]==b'EXFAT   ' and ex[12*bps+3:12*bps+11]==b'EXFAT   '
        volume_flags=rng.randrange(0,8)
        percent=rng.randrange(0,101)
        # Conversion destroys sectors 0..31, which necessarily covers exFAT's
        # Main+Backup Boot Regions (sectors 0..23). We validate replacement
        # metadata directly rather than allocating/copying 32 sectors each loop.
        assert 24 <= RS
        part_off=(0,2048,4096,32768)[rng.randrange(4)]
        label=f'T{rng.getrandbits(36):09X}'[:11]
        boot,fs,fat0,root=make_fat32_metadata(layout,part_off,rng.getrandbits(32),label)
        validate_fat32(layout,boot,fs,fat0,root,part_off,label)
        # Both primary and backup FAT32 structures are byte-identical in v0.2.1.
        assert boot[3:11] != b'EXFAT   ' and boot[82:90]==b'FAT32   '
        assert fs[0:4] == struct.pack('<I',0x41615252)
        digest.update(struct.pack('<IIIIQBB', bps,layout['cluster_bytes'],layout['fat'],layout['spc'],layout['clusters'],volume_flags,percent))
        digest.update(boot[:90]); digest.update(fs[484:496]); digest.update(fat0[:12])
    elapsed=time.perf_counter()-started
    return dict(capacity=name,bytes=size,loops=loops,elapsed_seconds=elapsed,iterations_per_second=loops/elapsed,digest=digest.hexdigest(),valid_cluster_modes=valid_clusters)

def zero_range_file(f, offset, nbytes, chunk=8*1024*1024):
    z=b'\x00'*chunk
    f.seek(offset)
    left=nbytes
    while left:
        n=min(left,chunk); f.write(z[:n]); left-=n

def full_image_convert(path, name, size, bps=512, cluster_bytes=0):
    vol=size-size%bps
    ex=build_exfat_boot_region(vol,bps,0xA1B2C3D4,37,2048,128*1024)
    with open(path,'wb') as f:
        f.truncate(vol); f.seek(0); f.write(ex); f.flush()
    pre=subprocess.run(['blkid','-p','-o','export',str(path)],capture_output=True,text=True)
    if pre.returncode != 0 or 'TYPE=exfat' not in pre.stdout:
        raise RuntimeError(f'{name}: source not recognized as exFAT: {pre.stdout} {pre.stderr}')
    layout=fat32_layout(vol,bps,cluster_bytes)
    boot,fs,fat0,root=make_fat32_metadata(layout,2048,0xC0FFEE01,'FAT32TEST')
    with open(path,'r+b',buffering=0) as f:
        zero_range_file(f,0,RS*bps)
        f.seek(0); f.write(boot); f.seek(bps); f.write(fs); f.seek(6*bps); f.write(boot); f.seek(7*bps); f.write(fs)
        zero_range_file(f,layout['fat1'],layout['fat_bytes'])
        zero_range_file(f,layout['fat2'],layout['fat_bytes'])
        f.seek(layout['fat1']); f.write(fat0); f.seek(layout['fat2']); f.write(fat0)
        zero_range_file(f,layout['root'],layout['cluster_bytes'])
        f.seek(layout['root']); f.write(root)
        f.flush(); os.fsync(f.fileno())
    post=subprocess.run(['blkid','-p','-o','export',str(path)],capture_output=True,text=True)
    if post.returncode != 0 or 'TYPE=vfat' not in post.stdout or 'VERSION=FAT32' not in post.stdout:
        raise RuntimeError(f'{name}: target not recognized FAT32: {post.stdout} {post.stderr}')
    with open(path,'rb') as f:
        r=f.read(RS*bps)
        assert r[3:11] != b'EXFAT   '
        assert r[82:90] == b'FAT32   '
        assert r[6*bps:7*bps] == r[0:bps]
        assert r[7*bps:8*bps] == r[bps:2*bps]
        for off in (layout['fat1'],layout['fat2']):
            f.seek(off); s=f.read(bps); assert s[:12]==fat0[:12]
            # sample middle/end of each FAT to ensure old metadata was cleared
            for pos in (off+layout['fat_bytes']//2, off+layout['fat_bytes']-bps):
                f.seek(pos); assert f.read(bps)==b'\x00'*bps
        f.seek(layout['root']); rr=f.read(layout['cluster_bytes']); assert rr[:12]==root[:12]
    return dict(capacity=name,bytes=vol,bps=bps,layout=layout,pre_blkid=pre.stdout.strip().splitlines(),post_blkid=post.stdout.strip().splitlines())


def main():
    ap=argparse.ArgumentParser()
    ap.add_argument('--loops',type=int,default=100000)
    ap.add_argument('--full-images',action='store_true')
    ap.add_argument('--outdir',default='/mnt/data/fat32test/exfat_stress_images')
    args=ap.parse_args()
    if args.loops < 100000:
        print('WARNING: requested fewer than 100000 loops per capacity',file=sys.stderr)
    allres=[]
    base_seed=0xEFA732021
    for idx,(name,size) in enumerate(CAPACITIES.items()):
        r=stress_one(name,size,args.loops,base_seed+idx)
        allres.append(r)
        print('STRESS_PASS',name,args.loops,f"{r['elapsed_seconds']:.3f}s",r['digest'])
    full=[]
    if args.full_images:
        out=Path(args.outdir); out.mkdir(parents=True,exist_ok=True)
        for name,size in CAPACITIES.items():
            p=out/f'{name}.img'
            t=time.perf_counter(); r=full_image_convert(p,name,size,512,0); r['elapsed_seconds']=time.perf_counter()-t
            full.append(r)
            print('FULL_IMAGE_PASS',name,f"{r['elapsed_seconds']:.3f}s")
            p.unlink() # avoid retaining multi-TB sparse files
    summary={'stress':allres,'full_images':full,'total_stress_loops':sum(x['loops'] for x in allres)}
    print('SUMMARY_JSON='+json.dumps(summary,separators=(',',':')))

if __name__=='__main__': main()
