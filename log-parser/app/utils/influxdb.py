from influxdb_client import Point
from influxdb_client.rest import ApiException
from influxdb_client.client.influxdb_client_async import InfluxDBClientAsync
import logging

class InfluxWriterAsync:
    def __init__(self, url: str, token: str, org: str, bucket: str):
        self.client = InfluxDBClientAsync(url=url, token=token, org=org, timeout=5000)
        self.bucket = bucket
        self.org = org
        self.write_api = self.client.write_api()

    async def write(self, point: Point):
        try:
            await self.write_api.write(bucket=self.bucket, org=self.org, record=point)
        except ApiException as e:
            logging.error(f"InfluxDB write failed: {e}")
            raise

    async def close(self):
        await self.client.__aexit__(None, None, None)
