export namespace main {
	
	export class HeroRef {
	    id: number;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new HeroRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class Snapshot {
	    status: string;
	    gameState: string;
	    own: HeroRef[];
	    enemy: HeroRef[];
	    recommendations: rules.Recommendation[];
	    serverUrl: string;
	    configPath: string;
	    configInstalled: boolean;
	    candidates: string[];
	    clickThrough: boolean;
	    clickThroughSupported: boolean;
	    enemyManual: boolean;
	    ownManual: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.gameState = source["gameState"];
	        this.own = this.convertValues(source["own"], HeroRef);
	        this.enemy = this.convertValues(source["enemy"], HeroRef);
	        this.recommendations = this.convertValues(source["recommendations"], rules.Recommendation);
	        this.serverUrl = source["serverUrl"];
	        this.configPath = source["configPath"];
	        this.configInstalled = source["configInstalled"];
	        this.candidates = source["candidates"];
	        this.clickThrough = source["clickThrough"];
	        this.clickThroughSupported = source["clickThroughSupported"];
	        this.enemyManual = source["enemyManual"];
	        this.ownManual = source["ownManual"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace rules {
	
	export class Recommendation {
	    heroId: number;
	    name: string;
	    tags: string[];
	    score: number;
	    reasons: string[];
	
	    static createFrom(source: any = {}) {
	        return new Recommendation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.heroId = source["heroId"];
	        this.name = source["name"];
	        this.tags = source["tags"];
	        this.score = source["score"];
	        this.reasons = source["reasons"];
	    }
	}

}

