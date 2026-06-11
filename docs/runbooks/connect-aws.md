# Connect AWS — read-only access for Vesta

Vesta drills your existing backup dumps; it never touches your production
database. To do that we need **read-only** access to the S3 bucket your
backup job writes to. This page is the doc you can hand your devops or
security team to deploy that access in ~3 minutes.

The mechanism is the same one used by Datadog, Snowflake, Vanta, and Drata:
a **cross-account IAM role** in your AWS account that Vesta's account is
allowed to assume. No long-lived access keys leave your account. Access is
revocable in one click (delete the CloudFormation stack).

## What Vesta can do with this role

- `s3:GetObject` on the bucket and prefix you specify (read your backup dumps).
- `s3:ListBucket` + `s3:GetBucketLocation` on that bucket (find new dumps).

## What Vesta cannot do

- Read any other bucket.
- Write, modify, or delete anything in your AWS account.
- Connect to any of your databases.
- Use this role from anywhere except Vesta's AWS account, and only when
  it presents the `ExternalId` Vesta generated for your tenant (defeats
  the confused-deputy class of attacks).

## The CloudFormation template

Save this as `selket-connect.yaml`, or use the in-app "Connect AWS"
button which links to a hosted copy with your `ExternalId` pre-filled.

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: >
  Vesta backup-drilling read access. Creates a cross-account IAM role
  Vesta can assume to fetch backup dumps from one S3 bucket/prefix.

Parameters:
  BackupBucket:
    Type: String
    Description: Name of the S3 bucket containing your backup dumps (no s3:// prefix).
  BackupPrefix:
    Type: String
    Default: ""
    Description: Optional path prefix; leave empty to allow the whole bucket.
  ExternalId:
    Type: String
    Description: The external ID Vesta gave you when you clicked "Connect AWS".
    NoEcho: true
  VestaAccountId:
    Type: String
    Default: "REPLACE_WITH_SELKET_ACCOUNT_ID"
    Description: The AWS account ID Vesta runs in. Comes pre-filled from the dashboard.

Resources:
  VestaReadRole:
    Type: AWS::IAM::Role
    Properties:
      RoleName: VestaBackupRead
      Description: Read-only access for Vesta to drill backups in one S3 bucket.
      MaxSessionDuration: 3600
      AssumeRolePolicyDocument:
        Version: '2012-10-17'
        Statement:
          - Effect: Allow
            Principal:
              AWS: !Sub "arn:aws:iam::${VestaAccountId}:root"
            Action: sts:AssumeRole
            Condition:
              StringEquals:
                sts:ExternalId: !Ref ExternalId
      Policies:
        - PolicyName: VestaBackupRead
          PolicyDocument:
            Version: '2012-10-17'
            Statement:
              # Read the dump files themselves.
              - Effect: Allow
                Action:
                  - s3:GetObject
                  - s3:GetObjectVersion
                Resource: !Sub "arn:aws:s3:::${BackupBucket}/${BackupPrefix}*"
              # List the bucket so we can find new dumps. The s3:prefix
              # condition restricts listing to the chosen path.
              - Effect: Allow
                Action:
                  - s3:ListBucket
                  - s3:GetBucketLocation
                Resource: !Sub "arn:aws:s3:::${BackupBucket}"
                Condition:
                  StringLike:
                    s3:prefix:
                      - !Sub "${BackupPrefix}*"

Outputs:
  RoleArn:
    Description: Paste this ARN into the Vesta dashboard to finish the connection.
    Value: !GetAtt VestaReadRole.Arn
```

## Deploy it — step by step

1. In the Vesta dashboard, go to **Databases → Add database → Connect AWS**.
   Vesta displays your **ExternalId** (a per-tenant secret) and a
   pre-filled "Launch Stack" link.
2. Click **Launch Stack**. AWS opens the CloudFormation **Create stack**
   wizard in your browser, on the right account/region, with the template
   already loaded.
3. Fill in:
   - `BackupBucket` — the bucket name (e.g. `acme-prod-backups`).
   - `BackupPrefix` — optional path (e.g. `postgres/prod/`); leave empty
     to allow the whole bucket.
   - `ExternalId` — the one from the dashboard.
4. Check **I acknowledge that AWS CloudFormation might create IAM
   resources with custom names**, then **Create stack**.
5. Wait ~30 seconds for the stack to reach `CREATE_COMPLETE`.
6. Open the stack's **Outputs** tab and copy `RoleArn`.
7. Paste the role ARN back into the Vesta dashboard. Vesta
   immediately runs a connectivity probe (an `sts:AssumeRole` round-trip
   and a `HeadObject` against the bucket) and shows green when it works.

## Verifying the connection later

In the dashboard, **Databases → Settings → Test connection** re-runs the
probe. If it fails the dashboard prints AWS's exact error message, so
diagnosing a mistyped bucket name or revoked role is one read.

## Revoking access

Delete the CloudFormation stack. The role goes with it, and any future
Vesta assume-role attempt receives `AccessDenied` instantly. Existing
evidence already in our vault remains accessible to you in the dashboard;
no new drills will run for the affected database.

## For your security review

**Authentication.** Vesta's account is the only `Principal` allowed to
assume the role, and only with the matching `ExternalId`. We do not store
your AWS credentials; we mint a fresh STS session per drill (≤ 1 hour
lifetime).

**Data egress.** During a drill the dump file is streamed into a fresh,
isolated Postgres instance on Vesta's runner, restored, asserted against,
and deleted with the runner VM at teardown. The signed PDF and a hash
fingerprint of the dump are retained in our evidence vault — the dump
itself is not.

**At-rest encryption.** Every evidence PDF is encrypted under a
per-account 256-bit data-encryption key, which is itself wrapped under our
master key. Deleting your account crypto-shreds the key and renders all
your evidence permanently unrecoverable, even from backups.

**Audit log.** Every assume-role event lands in your CloudTrail. Every
drill, evidence download, and account action lands in Vesta's
tamper-evident audit log (hash-chained, retained 7 years).

**Compliance.** Vesta's own SOC 2 Type I work is in progress; the
deployment runs on Fly.io (SOC 2 Type 2, ISO 27001) and Neon (SOC 2 Type
2). Subprocessor list available on request.

**Revocation.** One CloudFormation delete revokes us instantly. There is
no agent to uninstall.

## Not on AWS?

- **Google Cloud Storage:** create a service account with
  `storage.objects.get` + `storage.objects.list` on one bucket; share its
  key file. We support the same external-ID pattern via workload identity
  federation — ask us for the script.
- **Azure Blob Storage:** generate a read-only **SAS token** scoped to one
  container; paste it into the dashboard.
- **Cloudflare R2:** create a read-only **API token** scoped to one bucket
  via the R2 dashboard.
- **SFTP / on-prem:** SSH key authentication to a read-only account.
  Recommended only for enterprise customers; talk to us about the
  self-hosted-runner option, which keeps the dump inside your network and
  uploads only the signed-PDF metadata back to us.

## FAQ

**Why a role and not just an access key?**
Long-lived access keys are the #1 source of cloud breaches. A role
issued via `sts:AssumeRole` is short-lived, scoped, and revocable in one
click. Same reason every modern B2B SaaS asks for a role.

**Why the `ExternalId`?**
It defeats the *confused-deputy* attack: even if a third party learns our
account ID, they cannot assume your role without also presenting the
ExternalId we generated for *your* tenant.

**Can I scope this tighter?**
Yes — the `BackupPrefix` parameter limits both Get and List to a path
within the bucket. You can also add KMS-key conditions if your bucket
uses CMK encryption; ask us for the additional `kms:Decrypt` statement.

**What if my devops team isn't comfortable with CloudFormation?**
We have an equivalent **Terraform module** (`selket_aws_connect`) on
request, and the underlying IAM is plain enough that a security engineer
can hand-roll it from this doc in five minutes.
